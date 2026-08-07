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
	if err := os.WriteFile(manifestPath, []byte(manifest.String()), 0o644); err != nil {
		return nil, err
	}

	resultsPath := filepath.Join(absWork, "results.tsv")
	if err := invoke(ctx, script, cfg, absWork, manifestPath, resultsPath); err != nil {
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
// environment. Exit 1 is a normal outcome (FAIL rows exist — those are
// verdicts, not errors); anything else nonzero is an infrastructure error.
func invoke(ctx context.Context, script string, cfg Config, absWork, manifestPath, resultsPath string) error {
	lakeBudget := cfg.LakeBuildTimeout
	if lakeBudget <= 0 {
		lakeBudget = 20 * time.Minute
	}
	cmd := exec.CommandContext(ctx, "bash", script, manifestPath)
	cmd.Dir = cfg.Checkout
	env := []string{
		"GOLEAN_COVERAGE_ARTIFACTS=" + filepath.Join(absWork, "artifacts"),
		"GOLEAN_COVERAGE_RESULTS=" + resultsPath,
		"GOLEAN_COVERAGE_META=" + resultsPath + ".meta",
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
	if err != nil {
		if ee, isExit := err.(*exec.ExitError); isExit && ee.ExitCode() == 1 {
			return nil // FAIL rows present; results.tsv is still authoritative
		}
		return fmt.Errorf("diff-coverage: %w: %s", err, tail(out, 2000))
	}
	return nil
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
//     go-observation: THEIR gc oracle disagreed with the row we wrote, and
//     both sides are gc — the translation itself is broken: harness-error.
//   - An unknown stage (their vocabulary grew) is harness-error with the
//     stage preserved — explicit unclassifiable, never a guessed verdict.
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
