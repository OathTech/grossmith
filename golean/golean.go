// Package golean is the quarantined GoLean integration — the Phase 1
// clone adapter, shape (b) of the design note
// (docs/2026-08-06_observation-protocol-and-adapters.md §5): grossmith
// translates its cases into GoLean's differential-coverage corpus format,
// invokes their scripts/diff-coverage, and parses the closed result
// vocabulary back into harness verdicts. The Go-vs-Lean comparison happens
// INSIDE GoLean's harness against `go run`; grossmith's own gc reference
// pass supplies the expected status each manifest row requires.
//
// Everything GoLean-specific lives here. The rest of grossmith knows only
// gen.Config profiles (Profile) and harness.Verdict.
package golean

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"grossmith/gen"
	"grossmith/harness"
	"grossmith/observe"
)

// Config locates and paces the GoLean checkout.
type Config struct {
	// Checkout is the GoLean repo root; it must contain
	// scripts/diff-coverage and build with lake.
	Checkout string
	// Jobs is diff-coverage's per-case worker count (0 = their default,
	// nproc).
	Jobs int
	// LakeBuildTimeout raises GoLean's lake-build budget: a cold checkout
	// builds for minutes, and their default budget is 120s. 0 means 20m.
	LakeBuildTimeout time.Duration
}

// Profile applies GoLean's capability profile to a generator config
// (design note §8.2): their harness observes bool/int/uint/string/array/
// struct/interface but fails closed on slices and maps, and their machine
// has no model of the grossmith obs* event API — so slices and maps leave
// the observed tier (feeder/dead stay legal) and the event-emitting
// constructs are excluded. Everything else generates unchanged.
func Profile(cfg gen.Config) gen.Config {
	cfg.NoObserve = []gen.Shape{gen.ShapeSlice, gen.ShapeMap}
	// recover_wrapper is deliberately NOT excluded (Phase 4 rung 1, their
	// R1 request): the wrapper observes panics through named results —
	// pure Go, no obs* events — so defer/recover semantics reach their
	// machine despite the event exclusions below.
	cfg.Exclude = []string{"observe_point", "defer", "recover"}
	return cfg
}

// Case is one grossmith case handed to a GoLean campaign, with the gc
// reference outcome that supplies the row's expected status.
type Case struct {
	ID        string
	Dir       string // contains subject.go
	Features  []string
	Reference harness.Outcome
}

// Result is one case's verdict as judged through GoLean's harness. Stage
// is GoLean's classification stage, recorded verbatim ("" on PASS).
type Result struct {
	Verdict harness.Verdict
	Stage   string
	Detail  string
}

// Identity is the checkout's pinned identity: git commit, with a -dirty
// suffix when the working tree differs.
func Identity(ctx context.Context, checkout string) (string, error) {
	rev := exec.CommandContext(ctx, "git", "-C", checkout, "rev-parse", "HEAD")
	out, err := rev.Output()
	if err != nil {
		return "", fmt.Errorf("golean identity: %w", err)
	}
	id := "golean@" + strings.TrimSpace(string(out))
	status := exec.CommandContext(ctx, "git", "-C", checkout, "status", "--porcelain")
	if s, err := status.Output(); err == nil && len(bytes.TrimSpace(s)) > 0 {
		id += "-dirty"
	}
	return id, nil
}

// obsCallRe matches grossmith observation-event calls: a subject containing
// one reached this adapter without the Profile applied — a campaign
// misconfiguration, failed closed per case rather than shipped to a harness
// that cannot model the calls.
var obsCallRe = regexp.MustCompile(`\bobs(Bool|Int|Uint|Str|Recovered)\(`)

// Run translates cases into workDir, invokes diff-coverage once, and
// returns a verdict per case ID. workDir must be inside a directory
// already fenced from the repo module (the campaign root's throwaway
// go.mod): translated cases are .go files.
func Run(ctx context.Context, workDir string, cases []Case, cfg Config) (map[string]Result, error) {
	// Absolute from here on: the script runs with the checkout as its
	// working directory, so a relative checkout path would re-resolve
	// against itself.
	abs, err := filepath.Abs(cfg.Checkout)
	if err != nil {
		return nil, err
	}
	cfg.Checkout = abs
	script := filepath.Join(cfg.Checkout, "scripts", "diff-coverage")
	if _, err := os.Stat(script); err != nil {
		return nil, fmt.Errorf("golean checkout %s: %w", cfg.Checkout, err)
	}
	absWork, err := filepath.Abs(workDir)
	if err != nil {
		return nil, err
	}
	caseRoot := filepath.Join(absWork, "cases")
	if err := os.MkdirAll(caseRoot, 0o755); err != nil {
		return nil, err
	}

	results := make(map[string]Result, len(cases))
	var manifest strings.Builder
	manifest.WriteString("# id\tgo_dir\tfunction\targs\texpected_status\tfeatures\texpected_reason\tlane\twhy\tparams\n")
	translated := map[string]bool{}
	for _, c := range cases {
		row, res, ok := translate(caseRoot, c)
		if !ok {
			results[c.ID] = res
			continue
		}
		manifest.WriteString(row)
		translated[c.ID] = true
	}
	if len(translated) == 0 {
		return results, nil
	}
	manifestPath := filepath.Join(absWork, "manifest.tsv")
	manifestBytes := []byte(manifest.String())
	if err := os.WriteFile(manifestPath, manifestBytes, 0o644); err != nil {
		return nil, err
	}

	// A stale results file must never be readable as this run's verdicts
	// (audit F1, demonstrated: diff-coverage exits 1 BOTH for legitimate
	// FAIL rows and for a lake-build failure that publishes nothing — with
	// index-based case IDs, a previous campaign's results.tsv passed every
	// id check and its verdicts were republished as this run's conformance
	// statement). Freshness is enforced twice: the old files are removed
	// before the run, and the published meta must name the manifest we
	// just wrote.
	resultsPath := filepath.Join(absWork, "results.tsv")
	metaPath := resultsPath + ".meta"
	for _, p := range []string{resultsPath, metaPath} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return nil, err
		}
	}
	if err := invoke(ctx, script, cfg, absWork, manifestPath, resultsPath, metaPath); err != nil {
		return nil, err
	}
	if err := checkMeta(metaPath, manifestBytes); err != nil {
		return nil, err
	}

	rows, err := parseResults(resultsPath, translated)
	if err != nil {
		return nil, err
	}
	for id := range translated {
		res, ok := rows[id]
		if !ok {
			// diff-coverage fails closed per row, so a missing id means the
			// run itself is not trustworthy — abort, never default a verdict.
			return nil, fmt.Errorf("golean results: case %s missing from %s", id, resultsPath)
		}
		results[id] = res
	}
	return results, nil
}

// translate writes one case's GoLean tree and returns its manifest row.
// ok=false means the case cannot reach GoLean; res says why.
func translate(caseRoot string, c Case) (row string, res Result, ok bool) {
	if c.Reference.Status != harness.StatusRan {
		return "", Result{Verdict: harness.VerdictRefInfra,
			Detail: fmt.Sprintf("reference %s: %s", c.Reference.Status, c.Reference.Detail)}, false
	}
	doc := c.Reference.Document
	status, reason := "", "-"
	switch doc.Status {
	case observe.StatusOK:
		status = "ok"
	case observe.StatusPanic:
		status = "panic"
		if doc.Panic == nil || doc.Panic.Message == "" {
			return "", Result{Verdict: harness.VerdictHarnessError,
				Detail: "reference panicked without a message — no expected_reason to pin"}, false
		}
		reason = doc.Panic.Message
		if strings.ContainsAny(reason, "\t\n\r") {
			return "", Result{Verdict: harness.VerdictHarnessError,
				Detail: fmt.Sprintf("panic message not manifest-safe: %q", reason)}, false
		}
	default:
		return "", Result{Verdict: harness.VerdictRefInfra,
			Detail: fmt.Sprintf("reference document status %s", doc.Status)}, false
	}

	subject, err := os.ReadFile(filepath.Join(c.Dir, "subject.go"))
	if err != nil {
		return "", Result{Verdict: harness.VerdictHarnessError, Detail: err.Error()}, false
	}
	if m := obsCallRe.Find(subject); m != nil {
		return "", Result{Verdict: harness.VerdictHarnessError,
			Detail: fmt.Sprintf("subject calls %s but GoLean has no obs* model — generate with golean.Profile", strings.TrimSuffix(string(m), "("))}, false
	}
	dir := filepath.Join(caseRoot, c.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", Result{Verdict: harness.VerdictHarnessError, Detail: err.Error()}, false
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), subject, 0o644); err != nil {
		return "", Result{Verdict: harness.VerdictHarnessError, Detail: err.Error()}, false
	}

	features := strings.Join(c.Features, ",")
	if features == "" {
		features = "none"
	}
	row = strings.Join([]string{
		c.ID, dir, gen.Subject, "-", status, features, reason, "strict", "-", "-",
	}, "\t") + "\n"
	return row, Result{}, true
}

// invoke runs diff-coverage against the manifest with a sanitized
// environment, persisting its full output as a campaign artifact. Exit 1
// alone does NOT mean FAIL rows exist — the script also exits 1 on
// no-publish paths (lake build failure) — so invoke only distinguishes
// "ran to some exit" from hard errors; freshness and publication are
// checked by the caller against the meta file.
func invoke(ctx context.Context, script string, cfg Config, absWork, manifestPath, resultsPath, metaPath string) error {
	lakeBudget := cfg.LakeBuildTimeout
	if lakeBudget <= 0 {
		lakeBudget = 20 * time.Minute
	}
	cmd := exec.CommandContext(ctx, "bash", script, manifestPath)
	cmd.Dir = cfg.Checkout
	env := []string{
		"GOLEAN_COVERAGE_ARTIFACTS=" + filepath.Join(absWork, "artifacts"),
		"GOLEAN_COVERAGE_RESULTS=" + resultsPath,
		"GOLEAN_COVERAGE_META=" + metaPath,
		fmt.Sprintf("LAKE_BUILD_TIMEOUT_SECONDS=%d", int(lakeBudget.Seconds())),
		"GOTOOLCHAIN=local", "CGO_ENABLED=0",
	}
	if cfg.Jobs > 0 {
		env = append(env, fmt.Sprintf("GOLEAN_COVERAGE_JOBS=%d", cfg.Jobs))
	}
	for _, key := range []string{"PATH", "HOME", "TMPDIR", "GOCACHE", "GOPATH", "GOMODCACHE", "ELAN_HOME"} {
		if v := os.Getenv(key); v != "" {
			env = append(env, key+"="+v)
		}
	}
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	// The script's own narration (lake failures, worker-pool warnings, the
	// pass/fail summary) is a campaign artifact, not noise (audit F2).
	if werr := os.WriteFile(filepath.Join(absWork, "diff-coverage.log"), out, 0o644); werr != nil {
		return werr
	}
	if err != nil {
		if ee, isExit := err.(*exec.ExitError); isExit && ee.ExitCode() == 1 {
			return nil // FAIL rows or a no-publish failure; checkMeta decides
		}
		return fmt.Errorf("diff-coverage: %w: %s", err, tail(out, 2000))
	}
	return nil
}

// checkMeta requires the run to have PUBLISHED results for exactly the
// manifest we wrote: diff-coverage records the manifest sha256 in its meta
// file at publish time, so a missing meta means the run died before
// publishing and a mismatched sha means the results belong to some other
// manifest. Either way the results file is not this campaign's verdicts.
func checkMeta(metaPath string, manifest []byte) error {
	b, err := os.ReadFile(metaPath)
	if err != nil {
		return fmt.Errorf("golean: diff-coverage published no results (lake build failure or early exit) — see diff-coverage.log: %w", err)
	}
	want := fmt.Sprintf("%x", sha256.Sum256(manifest))
	for _, line := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(line, "\t")
		if ok && k == "manifest_sha256" {
			if v != want {
				return fmt.Errorf("golean: published results are for manifest %s, not ours (%s)", v, want)
			}
			return nil
		}
	}
	return fmt.Errorf("golean: meta file %s has no manifest_sha256", metaPath)
}

// parseResults reads GoLean's results TSV fail-closed: exact header, five
// fields per row, closed result vocabulary, no unknown or duplicate IDs.
func parseResults(path string, known map[string]bool) (map[string]Result, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) == 0 || lines[0] != "result\tid\tfeatures\tstage\tdetail" {
		return nil, fmt.Errorf("golean results %s: unrecognized header", path)
	}
	out := map[string]Result{}
	for _, line := range lines[1:] {
		f := strings.Split(line, "\t")
		if len(f) != 5 {
			return nil, fmt.Errorf("golean results: row has %d fields, want 5: %q", len(f), line)
		}
		result, id, stage, detail := f[0], f[1], f[3], f[4]
		if !known[id] {
			return nil, fmt.Errorf("golean results: unknown case id %q", id)
		}
		if _, dup := out[id]; dup {
			return nil, fmt.Errorf("golean results: duplicate case id %q", id)
		}
		res, err := judge(result, stage, detail)
		if err != nil {
			return nil, err
		}
		out[id] = res
	}
	return out, nil
}

// judge maps GoLean's (result, stage) vocabulary onto harness verdicts.
//
//   - PASS: their harness compared Go and the machine and they agreed —
//     match.
//   - differential / lean-observation / nondet: the machine disagreed with
//     Go (wrong value, wrong status, or iteration-order variance) —
//     observation-mismatch, the semantic signal.
//   - frontend-export / lean-run / harness: GoLean could not evaluate the
//     case (frontend coverage gap, nonzero machine exit, dead worker) —
//     clone-infra-failure, never conflated with divergence.
//   - manifest / go-source / litmus-contract / go-harness / go-run /
//     go-observation: the go SIDE of their harness failed — usually a
//     translation defect (both oracles are gc, so a status disagreement
//     means we corrupted the case), but go-harness/go-run also fire on
//     their go-side infrastructure (a 30s timeout, an OOM under load), so
//     inspect the detail before hunting a translation bug (audit F7).
//     harness-error either way: a fail-closed red, never a semantic claim.
//   - An unknown stage (their vocabulary grew) is harness-error with the
//     stage preserved — explicit unclassifiable, never a guessed verdict.
//     The membership-lane stages are known-unreachable (translate pins
//     lane=strict) and deliberately unmapped; they fall through here.
func judge(result, stage, detail string) (Result, error) {
	switch result {
	case "PASS":
		return Result{Verdict: harness.VerdictMatch}, nil
	case "FAIL":
	default:
		return Result{}, fmt.Errorf("golean results: unknown result %q", result)
	}
	res := Result{Stage: stage, Detail: detail}
	switch stage {
	case "differential", "lean-observation", "nondet":
		res.Verdict = harness.VerdictMismatch
		// A machine that got STUCK or refused as UNSUPPORTED produced no
		// observation at all — the interpreter analogue of the
		// frontend-quarantined gap, and the same side of the
		// infra/semantics line (audit F6 for stuck; rung 1 added
		// unsupported when wrapped subjects' p.(error) asserts hit their
		// machine-level $runtime.Error refusal). The detail carries their
		// observation JSON verbatim, which is the only channel that
		// distinguishes a refusal from a wrong value.
		if stage == "lean-observation" &&
			(strings.Contains(detail, `"status":"stuck"`) || strings.Contains(detail, `"status":"unsupported"`)) {
			res.Verdict = harness.VerdictCloneInfra
		}
	case "frontend-export", "lean-run", "harness":
		res.Verdict = harness.VerdictCloneInfra
	case "manifest", "go-source", "litmus-contract", "go-harness", "go-run", "go-observation":
		res.Verdict = harness.VerdictHarnessError
	default:
		res.Verdict = harness.VerdictHarnessError
		res.Detail = fmt.Sprintf("unrecognized GoLean stage %q: %s", stage, detail)
	}
	return res, nil
}

func tail(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return "..." + string(b[len(b)-n:])
}
