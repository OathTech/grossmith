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
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
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
	// builds for minutes, and their default budget is 120s. 0 means
	// DefaultLakeBuildTimeout.
	LakeBuildTimeout time.Duration
	// GoBin, when set, PINS the go binary GoLean's script oracle uses
	// (evidence arc E2; audit P0: the script resolved `go` from ambient
	// PATH, so the value comparison could run against a different Go
	// than the recorded reference). deps/golean is read-only, so the pin
	// is a PATH SHIM: a private directory whose only entry is a `go`
	// symlink to this binary, placed FIRST on the script's PATH. Must be
	// an absolute path. Empty preserves the ambient resolution (and the
	// audit finding) — the CLI always sets it.
	GoBin string
}

// Campaign budgets, named so the batch report can record them (E5:
// the 16MB cap was an inline literal absent from BatchBudgets, and
// LakeBuildTimeout — the largest budget in the system — was unrecorded).
const (
	// DefaultLakeBuildTimeout is the lake-build budget when Config leaves
	// LakeBuildTimeout zero.
	DefaultLakeBuildTimeout = 20 * time.Minute
	// LogCap bounds diff-coverage's captured output.
	LogCap = 16 << 20
)

// RunCeiling is the hard outer bound on one diff-coverage invocation —
// deliberately generous, a backstop rather than a pace-setter: the build
// budget, a per-case allowance far above anything a passing case needs,
// and slack. The script paces itself; this exists so a wedged script
// cannot hang a campaign forever.
func RunCeiling(lakeBudget time.Duration, nCases int) time.Duration {
	return lakeBudget + time.Duration(nCases)*time.Minute + 10*time.Minute
}

// Profile applies GoLean's capability profile to a generator config
// (design note §8.2): their harness observes bool/int/uint/string/array/
// struct/interface but fails closed on slices and maps, and their machine
// has no model of the grossmith obs* event API — so slices and maps leave
// the observed tier (feeder/dead stay legal) and the event-emitting
// constructs are excluded. Everything else generates unchanged.
// The profile is a UNION with whatever the caller already set: it adds
// its masks and exclusions and never drops one (2026-08-10 audit,
// P1/P2: it assigned both slices, so a caller who had masked another
// shape or excluded another construct had that policy silently
// discarded — and a profile whose job is to REMOVE capability must not
// be able to restore any). Deterministic: masks in enum order,
// exclusions sorted and deduplicated, so a config's identity in
// batch.json does not depend on call order.
func Profile(cfg gen.Config) gen.Config {
	cfg.NoObserve = unionShapes(cfg.NoObserve, gen.ShapeSlice, gen.ShapeMap)
	// recover_wrapper is deliberately NOT excluded (Phase 4 rung 1, their
	// R1 request): the wrapper observes panics through named results —
	// pure Go, no obs* events — so defer/recover semantics reach their
	// machine despite the event exclusions below.
	cfg.Exclude = unionTags(cfg.Exclude, "observe_point", "defer", "recover")
	return cfg
}

func unionShapes(have []gen.Shape, add ...gen.Shape) []gen.Shape {
	seen := map[gen.Shape]bool{}
	for _, s := range append(append([]gen.Shape(nil), have...), add...) {
		seen[s] = true
	}
	var out []gen.Shape
	// Enum order, so the result is independent of input order.
	for _, s := range []gen.Shape{gen.ShapeInt, gen.ShapeBool, gen.ShapeString,
		gen.ShapeArray, gen.ShapeStruct, gen.ShapeMap, gen.ShapeInterface, gen.ShapeSlice} {
		if seen[s] {
			out = append(out, s)
			delete(seen, s)
		}
	}
	// Anything outside the enum is preserved rather than dropped;
	// Config.Validate is what refuses it, and it must see it.
	for s := range seen {
		out = append(out, s)
	}
	return out
}

func unionTags(have []string, add ...string) []string {
	seen := map[string]bool{}
	for _, t := range append(append([]string(nil), have...), add...) {
		seen[t] = true
	}
	out := make([]string, 0, len(seen))
	for t := range seen {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
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

// gitEnv is the sanitized environment for identity queries (hunt F3: an
// ambient GIT_DIR overrides -C discovery entirely, and a non-repo
// checkout lets git walk UP — both fabricated the pinned provenance).
func gitEnv() []string {
	env := []string{}
	for _, key := range []string{"PATH", "HOME"} {
		if v := os.Getenv(key); v != "" {
			env = append(env, key+"="+v)
		}
	}
	return env
}

// gitProbe runs one identity query under its own budget and group
// cancellation (E5, review C1: these probes ran on the caller's context,
// which is Background in the campaign path — a wedged git, e.g. one
// waiting on a filesystem monitor, hung the campaign before any case).
func gitProbe(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, harness.IdentityTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = gitEnv()
	harness.KillGroup(cmd)
	return cmd.Output()
}

// Identity is the checkout's pinned identity: git commit, with a -dirty
// suffix when the working tree differs (and -dirty-unknown when the
// dirtiness check itself failed — a failed check must not read as clean).
func Identity(ctx context.Context, checkout string) (string, error) {
	absC, err := filepath.Abs(checkout)
	if err != nil {
		return "", err
	}
	// The checkout must BE a repository root (or linked worktree root) —
	// otherwise git resolves some ancestor repo (measured: grossmith's
	// own HEAD recorded as the GoLean clone identity, err == nil).
	topOut, err := gitProbe(ctx, "-C", absC, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("golean identity: %s is not a git checkout: %w", absC, err)
	}
	if filepath.Clean(strings.TrimSpace(string(topOut))) != filepath.Clean(absC) {
		return "", fmt.Errorf("golean identity: %s is not a repository root (git resolves %s)",
			absC, strings.TrimSpace(string(topOut)))
	}
	out, err := gitProbe(ctx, "-C", absC, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("golean identity: %w", err)
	}
	id := "golean@" + strings.TrimSpace(string(out))
	if s, err := gitProbe(ctx, "-C", absC, "status", "--porcelain"); err != nil {
		id += "-dirty-unknown"
	} else if len(bytes.TrimSpace(s)) > 0 {
		id += "-dirty"
	}
	return id, nil
}

// obsCallRe matches grossmith observation-event calls: a subject containing
// one reached this adapter without the Profile applied — a campaign
// misconfiguration, failed closed per case rather than shipped to a harness
// that cannot model the calls.
var obsCallRe = regexp.MustCompile(`\bobs(Bool|Int|Uint|Str|Recovered)\(`)

// caseIDRe: the safe case-ID shape — path-component only, no
// separators, no dots (no traversal), TSV-safe.
var caseIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

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
	// PREVALIDATE THE WHOLE SLICE BEFORE ANY WRITE (2026-08-10 audit, P1,
	// reproduced: a duplicate ID first recorded a harness-error and then
	// had that entry OVERWRITTEN by the translated row's verdict when the
	// results were folded back in — `golean.Run` returned err=nil and
	// `match` for a case list it should have refused outright. A
	// whole-run precondition cannot be a per-case verdict, because the
	// map key is the same key.)
	if err := prevalidate(cases); err != nil {
		return nil, err
	}
	// The case tree starts EMPTY (2026-08-10 audit, P1/P2: cases were
	// written into whatever was already there, so a previous run's case
	// directories survived beside this run's and were caught only later,
	// by optional digest verification). A leftover must prove it is ours
	// before it is removed — the same content-test discipline the batch
	// output directory uses.
	caseRoot := filepath.Join(absWork, "cases")
	if err := clearCaseRoot(absWork); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(caseRoot, 0o755); err != nil {
		return nil, err
	}
	// The marker is written with the tree, so the NEXT run can prove this
	// one's leftovers are ours.
	if err := os.WriteFile(filepath.Join(absWork, workMarker), []byte(workMarkerContent(absWork)), 0o644); err != nil {
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
	// The ceiling is computed over the HANDED case count, not the
	// translated subset, so the producer can record the exact same
	// figure in the batch report without knowing translation outcomes.
	if err := invoke(ctx, script, cfg, absWork, manifestPath, resultsPath, metaPath, len(cases)); err != nil {
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

// workMarker names the ownership marker written at the work root before
// the first case is translated. Its CONTENT binds the marker to that
// exact work directory, so a leftover tree can be proven ours before it
// is cleared — the discipline the batch staging tree already uses. A
// name test alone cannot work here: case IDs are caller-chosen and the
// safe-ID shape is permissive, so a directory called `vendor` holding a
// `main.go` is indistinguishable from a translated case.
const workMarker = ".golean-work"

func workMarkerContent(absWork string) string {
	return "golean translated-case tree for " + absWork + " — safe to delete\n"
}

// clearCaseRoot empties the translated-case tree so a run never writes
// into another run's cases. It removes the tree only when the ownership
// marker proves it is ours and names THIS work directory; anything else
// — no marker, a marker for another directory, a symlink, a file — is
// left untouched and refuses the run.
func clearCaseRoot(absWork string) error {
	caseRoot := filepath.Join(absWork, "cases")
	fi, err := os.Lstat(caseRoot)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() {
		return fmt.Errorf("golean work: %s is not a directory — refusing to touch it", caseRoot)
	}
	b, err := os.ReadFile(filepath.Join(absWork, workMarker))
	if err != nil || string(b) != workMarkerContent(absWork) {
		return fmt.Errorf("golean work: %s exists but %s does not mark it as this run's work tree — refusing to clear it (point workDir at a fresh directory, or move this one aside)",
			caseRoot, workMarker)
	}
	return os.RemoveAll(caseRoot)
}

// prevalidate checks the RUN's preconditions — the properties that
// cannot be expressed as one case's verdict — before anything is
// written (2026-08-10 audit, P1). Per-case reasons a case cannot reach
// GoLean (a non-ran reference, an unsupported document status, an obs*
// call) stay per-case verdicts in translate; these are different: they
// mean the caller handed us a case LIST we cannot judge.
func prevalidate(cases []Case) error {
	seen := map[string]bool{}
	for i, c := range cases {
		if !caseIDRe.MatchString(c.ID) {
			return fmt.Errorf("golean: case %d has ID %q, which is not manifest-safe (want %s)", i, c.ID, caseIDRe)
		}
		if seen[c.ID] {
			return fmt.Errorf("golean: duplicate case ID %q — verdicts are keyed by ID, so a duplicate cannot be reported per case", c.ID)
		}
		seen[c.ID] = true
		for _, f := range c.Features {
			if strings.ContainsAny(f, "\t\n\r,") || f == "" {
				return fmt.Errorf("golean: case %s has feature tag %q, which is not manifest-safe", c.ID, f)
			}
		}
		// An exported ran Outcome carries a Document the caller built, so
		// it is an API input: validate the tagged union before its status
		// is trusted (reproduced: an empty-schema document with
		// status:"ok" AND a panic payload was translated and judged
		// `match`, because translate switched on Status alone).
		if c.Reference.Status == harness.StatusRan {
			if err := c.Reference.Document.Validate(); err != nil {
				return fmt.Errorf("golean: case %s reference document is invalid: %w", c.ID, err)
			}
		}
	}
	return nil
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

	// Feature-tag safety is a RUN precondition, checked by prevalidate
	// before any write (2026-08-10 audit: it used to be checked here,
	// after this case's main.go had already been written).
	features := strings.Join(c.Features, ",")
	if features == "" {
		features = "none"
	}
	row = strings.Join([]string{
		c.ID, dir, gen.Subject, "-", status, features, reason, "strict", "-", "-",
	}, "\t") + "\n"
	return row, Result{}, true
}

// goShim builds the pin directory: exactly one entry, `go`, a symlink to
// the pinned binary. Rebuilt idempotently per run.
func goShim(absWork, goBin string) (string, error) {
	if !filepath.IsAbs(goBin) {
		return "", fmt.Errorf("golean: GoBin %q must be absolute", goBin)
	}
	dir := filepath.Join(absWork, "goshim")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	link := filepath.Join(dir, "go")
	if err := os.Remove(link); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if err := os.Symlink(goBin, link); err != nil {
		return "", err
	}
	return dir, nil
}

// invoke runs diff-coverage against the manifest with a sanitized
// environment, persisting its full output as a campaign artifact. Exit 1
// alone does NOT mean FAIL rows exist — the script also exits 1 on
// no-publish paths (lake build failure) — so invoke only distinguishes
// "ran to some exit" from hard errors; freshness and publication are
// checked by the caller against the meta file.
func invoke(ctx context.Context, script string, cfg Config, absWork, manifestPath, resultsPath, metaPath string, nCases int) error {
	lakeBudget := cfg.LakeBuildTimeout
	if lakeBudget <= 0 {
		lakeBudget = DefaultLakeBuildTimeout
	}
	// The script enforces its own lake-build budget, but nothing bounded
	// the invocation as a whole (E5, review C1): a script that wedges
	// after the build — or ignores its budget — hung the campaign
	// forever.
	ctx, cancel := context.WithTimeout(ctx, RunCeiling(lakeBudget, nCases))
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", script, manifestPath)
	cmd.Dir = cfg.Checkout
	env := []string{
		"GOLEAN_COVERAGE_ARTIFACTS=" + filepath.Join(absWork, "artifacts"),
		"GOLEAN_COVERAGE_RESULTS=" + resultsPath,
		"GOLEAN_COVERAGE_META=" + metaPath,
		fmt.Sprintf("LAKE_BUILD_TIMEOUT_SECONDS=%d", int(lakeBudget.Seconds())),
		"GOTOOLCHAIN=local", "CGO_ENABLED=0",
		// Pin the go environment for THEIR go-run oracle too (hunt F1:
		// a $HOME/.config/go/env GOARCH/GOFLAGS silently overrides both
		// sides identically through the HOME passthrough).
		"GOENV=off", "GOWORK=off",
	}
	if cfg.Jobs > 0 {
		env = append(env, fmt.Sprintf("GOLEAN_COVERAGE_JOBS=%d", cfg.Jobs))
	}
	pathVal := os.Getenv("PATH")
	if cfg.GoBin != "" {
		shimDir, err := goShim(absWork, cfg.GoBin)
		if err != nil {
			return err
		}
		// Shim FIRST; the ambient tail stays for bash/lake/elan. Every
		// `go` the script resolves is now the pinned binary.
		pathVal = shimDir + string(os.PathListSeparator) + pathVal
	}
	if pathVal != "" {
		env = append(env, "PATH="+pathVal)
	}
	for _, key := range []string{"HOME", "TMPDIR", "GOCACHE", "GOPATH", "GOMODCACHE", "ELAN_HOME"} {
		if v := os.Getenv(key); v != "" {
			env = append(env, key+"="+v)
		}
	}
	cmd.Env = env
	harness.KillGroup(cmd)
	capped := harness.NewCappedBuffer(LogCap)
	cmd.Stdout, cmd.Stderr = capped, capped
	err := cmd.Run()
	out := capped.Bytes()
	if capped.Truncated() {
		out = append(out, []byte("\n[log truncated at the 16MB E4 cap]\n")...)
	}
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
		// machine-level $runtime.Error refusal).
		//
		// The semantic/infra boundary is decided by a TYPED status now,
		// not by matching free text (2026-08-10 audit, P1: it was a
		// suffix test against two literal JSON tails, so whitespace,
		// field order, an added field, or encoder evolution would have
		// turned the same structured refusal into a semantic divergence).
		// Unknown schema, unparseable document, or unknown status is
		// harness-error — explicitly unclassifiable, never a semantic
		// claim.
		if stage == "lean-observation" {
			res.Verdict, res.Detail = classifyLeanObservation(detail)
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

// CloneObservationSchema is the schema string GoLean's observation
// documents carry. Anything else is a protocol change we must not
// interpret.
const CloneObservationSchema = "golean-observation-v1"

// leanObservationPrefix is how their script builds a lean-observation
// failure detail: `report_fail ... "expected status $expected_status,
// got $lean_observation"` (deps/golean/scripts/diff-coverage). The
// document is therefore the text AFTER this separator — and it is
// captured with 2>&1, so it is not necessarily a document at all when
// their machine fails to produce one.
const leanObservationPrefix = ", got "

// cloneObservation is the part of GoLean's observation document that
// decides classification. Decoded NON-strictly on purpose: they may add
// fields (they vendor us, we do not gate their evolution), and an
// unknown FIELD is not a reason to refuse. An unknown SCHEMA or STATUS
// is, because those are what the classification reads.
type cloneObservation struct {
	Schema  string `json:"schema"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// cloneStatusProducedNoObservation is the closed set of statuses meaning
// their machine produced NO observation to compare — the infrastructure
// side of the line. Read from deps/golean/scripts/diff-coverage and
// their findings doc; anything outside these and the value-carrying set
// below is unclassifiable by construction.
var cloneStatusProducedNoObservation = map[string]bool{
	"stuck":       true, // the machine did not progress (audit F6)
	"unsupported": true, // a frontend-quarantined declaration refused
}

// cloneStatusCarriesObservation is the closed set of statuses meaning
// their machine DID produce an outcome: a status disagreement here is
// the semantic signal the campaign exists to find.
var cloneStatusCarriesObservation = map[string]bool{
	"ok":       true,
	"panic":    true,
	"deadlock": true,
	"race":     true,
	"error":    true,
}

// classifyLeanObservation decides the verdict for a lean-observation
// failure from the STRUCTURED document their script appends to the
// detail, and returns the detail to record.
func classifyLeanObservation(detail string) (harness.Verdict, string) {
	_, doc, found := strings.Cut(detail, leanObservationPrefix)
	if !found {
		return harness.VerdictHarnessError,
			"lean-observation detail does not carry an observation document (no " +
				strconv.Quote(leanObservationPrefix) + " separator): " + detail
	}
	var obs cloneObservation
	if err := json.Unmarshal([]byte(strings.TrimSpace(doc)), &obs); err != nil {
		// Their machine failed without emitting a document (the field is
		// captured with 2>&1, so this is their error text). No observation
		// exists, so no semantic comparison happened — unclassifiable
		// rather than a divergence.
		return harness.VerdictHarnessError,
			"lean-observation carried no decodable observation document: " + detail
	}
	if obs.Schema != CloneObservationSchema {
		return harness.VerdictHarnessError,
			"lean-observation document has schema " + strconv.Quote(obs.Schema) +
				", want " + strconv.Quote(CloneObservationSchema) + ": " + detail
	}
	switch {
	case cloneStatusProducedNoObservation[obs.Status]:
		return harness.VerdictCloneInfra, detail
	case cloneStatusCarriesObservation[obs.Status]:
		return harness.VerdictMismatch, detail
	default:
		return harness.VerdictHarnessError,
			"lean-observation document has unrecognized status " + strconv.Quote(obs.Status) +
				" (their vocabulary grew; classify it before trusting this campaign): " + detail
	}
}

func tail(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return "..." + string(b[len(b)-n:])
}
