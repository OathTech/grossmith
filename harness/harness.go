// Package harness is the conformance product boundary: runtime adapters,
// the verdict taxonomy, and durable batch/case artifacts (audit C1, C3, C5,
// H2). An adapter is anything that can take a case directory and produce an
// observation document; the harness compares documents under an explicit
// equivalence policy and never confuses runtime failure with semantic
// divergence.
package harness

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"grossmith/observe"
)

// OutcomeStatus is how one adapter's run of one case ended.
type OutcomeStatus string

const (
	StatusRan         OutcomeStatus = "ran"
	StatusBuildFailed OutcomeStatus = "build-failed"
	StatusRunFailed   OutcomeStatus = "run-failed"
	StatusTimeout     OutcomeStatus = "timeout"
	StatusAdapterErr  OutcomeStatus = "adapter-error"
)

// Outcome is one adapter's result for one case.
type Outcome struct {
	Status   OutcomeStatus    `json:"status"`
	Document observe.Document `json:"document,omitempty"`
	Detail   string           `json:"detail,omitempty"`
}

// Adapter runs cases under one implementation.
type Adapter interface {
	Name() string
	// Identity is the pinned implementation identity (toolchain path and
	// version, clone commit) — persisted in every batch artifact.
	Identity(ctx context.Context) (string, error)
	Run(ctx context.Context, caseDir string) Outcome
}

// Verdict is the per-case comparison result — a closed taxonomy (audit C5):
// infrastructure failure is never semantic divergence.
type Verdict string

const (
	VerdictMatch        Verdict = "match"
	VerdictMismatch     Verdict = "observation-mismatch"
	VerdictRefInfra     Verdict = "reference-infra-failure"
	VerdictCloneInfra   Verdict = "clone-infra-failure"
	VerdictBothInfra    Verdict = "both-infra-failure"
	VerdictHarnessError Verdict = "harness-error"
)

// Judge compares two outcomes under the policy.
func Judge(ref, clone Outcome, policy observe.PanicPolicy) (Verdict, string) {
	refOK := ref.Status == StatusRan
	cloneOK := clone.Status == StatusRan
	switch {
	case !refOK && !cloneOK:
		return VerdictBothInfra, ref.Detail + " / " + clone.Detail
	case !refOK:
		return VerdictRefInfra, ref.Detail
	case !cloneOK:
		return VerdictCloneInfra, clone.Detail
	}
	// The DOCUMENT status is the second infrastructure axis (audit H2): an
	// adapter can report StatusRan while its document says "error" — no
	// observation exists, so there is nothing semantic to compare. Without
	// this gate two error documents compared equal (a fabricated match) and
	// one error document against a real observation became a fabricated
	// observation-mismatch.
	refDocOK, cloneDocOK := !ref.Document.Failed(), !clone.Document.Failed()
	switch {
	case !refDocOK && !cloneDocOK:
		return VerdictBothInfra, docDetail(ref.Document) + " / " + docDetail(clone.Document)
	case !refDocOK:
		return VerdictRefInfra, docDetail(ref.Document)
	case !cloneDocOK:
		return VerdictCloneInfra, docDetail(clone.Document)
	}
	eq, err := observe.Equal(ref.Document, clone.Document, policy)
	if err != nil {
		return VerdictHarnessError, err.Error()
	}
	if eq {
		return VerdictMatch, ""
	}
	return VerdictMismatch, ""
}

func docDetail(d observe.Document) string {
	if d.Error != nil {
		return string(d.Error.Kind) + ": " + d.Error.Detail
	}
	return "error document"
}

// CaseRecord is the durable per-case metadata (audit H2): everything needed
// to identify, regenerate, and re-judge one case.
type CaseRecord struct {
	Schema        string         `json:"schema"`
	ID            string         `json:"id"`
	Seed          int64          `json:"seed"`
	GeneratorRev  string         `json:"generatorRev"`
	SubjectSHA256 string         `json:"subjectSha256"`
	// DriverSHA256 pins the driver too (audit F10; additive — absent in
	// records written before it existed, and readers treat absence as
	// "verify via observation only").
	DriverSHA256 string         `json:"driverSha256,omitempty"`
	Features     map[string]int `json:"features"`
	DrawTrace    []int          `json:"drawTrace"`
	// Config is the generator configuration the case was drawn under
	// (audit M5: seed alone is not a regeneration key — swarm, corner,
	// profile exclusions all change the program for a fixed seed). Typed
	// as the producer's config struct; opaque to the harness.
	Config any `json:"config,omitempty"`
}

const CaseSchema = "grossmith-case-v1"

// CaseResult is one case's judged result inside a batch report.
type CaseResult struct {
	ID            string  `json:"id"`
	SubjectSHA256 string  `json:"subjectSha256"`
	Verdict       Verdict `json:"verdict,omitempty"`
	Reference     Outcome `json:"reference"`
	Clone         *Outcome `json:"clone,omitempty"`
	Detail        string  `json:"detail,omitempty"`
}

// BatchReport is the conformance statement as a durable artifact (audit H2):
// stdout is a view, this file is the product.
type BatchReport struct {
	Schema            string       `json:"schema"`
	GeneratorRev      string       `json:"generatorRev"`
	Seeds             [2]int64     `json:"seeds"` // inclusive range
	ReferenceName     string       `json:"referenceName"`
	ReferenceIdentity string       `json:"referenceIdentity"`
	CloneName         string       `json:"cloneName,omitempty"`
	CloneIdentity     string       `json:"cloneIdentity,omitempty"`
	PanicPolicy       string       `json:"panicPolicy"`
	Started           string       `json:"started"`
	Cases             []CaseResult `json:"cases"`
	// Aggregates, computed over Cases.
	Total    int             `json:"total"`
	Verdicts map[Verdict]int `json:"verdicts,omitempty"`
	// WrapperCaught counts recover-wrapper subjects whose reference run
	// caught a panic (status ok, nonzero trailing panic code) — panics
	// PanicPaths cannot see, since the wrapper converts them to normal
	// returns (audit F2: rung 1 was pushing the headline panic metric the
	// wrong way with the catch invisible). Populated by the producer,
	// which knows the per-case features.
	WrapperCaught int `json:"wrapperCaught,omitempty"`
	// WrapperJudged counts the subset of WrapperCaught that reached a
	// SEMANTIC verdict (match/mismatch) — the 2026-08-08 review's G3:
	// generated, caught, and judged are three different numbers, and
	// conflating them let a profile incompatibility look like tested
	// coverage (19 caught, 0 judged went unnoticed). A POINTER: null in
	// the artifact when no clone judged (hunt F6 — an unconditional zero
	// on clone-less runs reproduced the exact signature the field was
	// added to detect), never omitted when a clone ran.
	WrapperJudged *int `json:"wrapperJudged"`
	// WrapperCloneInfra counts the subset of WrapperCaught whose verdict
	// was clone-infra-failure — the third leg of the wrapper accounting
	// (mid-arc review finding 1): caught == judged + cloneInfra must hold
	// EXACTLY when a clone judged. The nightly gates on that identity; a
	// slack bound against the GLOBAL clone-infra count admitted a
	// two-thirds judgement regression, because the quarantine population
	// the slack excused is disjoint from the wrapper population. Null
	// when no clone judged, like WrapperJudged.
	WrapperCloneInfra *int `json:"wrapperCloneInfra"`
	// CompositionJudged is Composition restricted to cases that reached a
	// semantic verdict (hunt F8: the G3 generated-vs-judged remedy applied
	// to every tag, not just the wrapper — the ratio per tag is the
	// judged-coverage rate campaign readers previously had to compute by
	// joining case.json files by hand). Nil when no clone judged.
	CompositionJudged map[string]int `json:"compositionJudged,omitempty"`
	// Pairs marks a pairs-mode batch (cases per construct pair); 0 for a
	// natural-population batch (hunt F11: the artifact was previously
	// indistinguishable from a plain batch).
	Pairs int `json:"pairsPerCombo,omitempty"`
	// ReferenceOracle / CloneNestedOracle: the STRUCTURED toolchain
	// identities (E2). ReferenceOracle is the binary the reference
	// adapter resolved and hashed; CloneNestedOracle is the SAME binary
	// as threaded into the clone's script oracle (plus the script hash) —
	// recorded so "which go did the value comparison" is answerable from
	// the artifact alone.
	ReferenceOracle   *OracleIdentity `json:"referenceOracle,omitempty"`
	CloneNestedOracle *OracleIdentity `json:"cloneNestedOracle,omitempty"`
	// Composition is the per-tag program-presence histogram — the charter
	// lists it as part of the conformance statement (rung 5 closed the
	// gap: it was stdout-only). Populated by the producer from generated
	// features.
	Composition map[string]int `json:"composition,omitempty"`
	RefRan      int             `json:"refRan"`
	PanicPaths  int             `json:"panicPaths"`
	Recovered   int             `json:"recovered"`
	// Subject size distribution (audit M4: "small" as a number).
	SubjectBytesMin  int `json:"subjectBytesMin,omitempty"`
	SubjectBytesMean int `json:"subjectBytesMean,omitempty"`
	SubjectBytesMax  int `json:"subjectBytesMax,omitempty"`
}

const BatchSchema = "grossmith-batch-v1"

// SubjectHash is the case identity hash.
func SubjectHash(subject []byte) string {
	h := sha256.Sum256(subject)
	return hex.EncodeToString(h[:])
}

// GcAdapter is the reference adapter: a PINNED go toolchain (explicit
// binary path — identity is the resolved path plus its reported version),
// building a case directory with a sanitized environment and running the
// binary with an EMPTY environment (an inherited GODEBUG makes identical
// binaries print differently).
type GcAdapter struct {
	// GoBin is the toolchain to use; empty resolves "go" from PATH. The
	// FIRST resolution is cached and pinned for the adapter's lifetime
	// (audit L3: re-resolving per run meant a PATH change mid-batch could
	// run cases under a toolchain the persisted identity never named).
	GoBin   string
	GOARCH  string
	Timeout time.Duration
	// name distinguishes multiple instances (reference vs degenerate clone).
	AdapterName string

	resolveOnce sync.Once
	resolved    string
	resolveErr  error
}

func (a *GcAdapter) Name() string {
	if a.AdapterName != "" {
		return a.AdapterName
	}
	return "gc"
}

func (a *GcAdapter) resolveGo() (string, error) {
	a.resolveOnce.Do(func() {
		bin := a.GoBin
		if bin == "" {
			bin = "go"
		}
		if strings.ContainsRune(bin, os.PathSeparator) {
			// Absolute NOW: build commands run with Dir set to the case
			// directory, and a relative path would re-resolve against it.
			a.resolved, a.resolveErr = filepath.Abs(bin)
			return
		}
		a.resolved, a.resolveErr = exec.LookPath(bin)
	})
	return a.resolved, a.resolveErr
}

func (a *GcAdapter) Identity(ctx context.Context) (string, error) {
	bin, err := a.resolveGo()
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, bin, "version")
	cmd.Env = a.buildEnv()
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s version: %w", bin, err)
	}
	id := strings.TrimSpace(string(out)) + " (" + bin
	// The EFFECTIVE arch, always — Identity previously named it only for
	// explicit cross-arch adapters, so an env-poisoned reference (hunt
	// F1) carried an identity that could not falsify the poison.
	arch := a.GOARCH
	if arch == "" {
		arch = runtime.GOARCH
	}
	id += ", GOARCH=" + arch
	return id + ")", nil
}

// OracleIdentity is the STRUCTURED toolchain identity (evidence arc E2;
// audit P0: the report named a reference the nested oracle need not have
// used). Path is absolute post-resolution; SHA256 is the binary's
// content hash — the field that makes "which go judged this batch"
// checkable offline. ScriptSHA256 is set only for nested script oracles.
type OracleIdentity struct {
	Path         string `json:"path"`
	SHA256       string `json:"sha256"`
	Version      string `json:"version"`
	GOOS         string `json:"goos"`
	GOARCH       string `json:"goarch"`
	ModuleMode   string `json:"moduleMode"`
	ScriptSHA256 string `json:"scriptSha256,omitempty"`
}

// Oracle returns the adapter's structured identity.
func (a *GcAdapter) Oracle(ctx context.Context) (OracleIdentity, error) {
	bin, err := a.resolveGo()
	if err != nil {
		return OracleIdentity{}, err
	}
	sum, err := FileSHA256(bin)
	if err != nil {
		return OracleIdentity{}, err
	}
	cmd := exec.CommandContext(ctx, bin, "version")
	cmd.Env = a.buildEnv()
	out, err := cmd.Output()
	if err != nil {
		return OracleIdentity{}, fmt.Errorf("%s version: %w", bin, err)
	}
	arch := a.GOARCH
	if arch == "" {
		arch = runtime.GOARCH
	}
	return OracleIdentity{
		Path: bin, SHA256: sum, Version: strings.TrimSpace(string(out)),
		GOOS: runtime.GOOS, GOARCH: arch,
		ModuleMode: "module (go 1.26)",
	}, nil
}

// FileSHA256 hashes a file's content — the oracle-identity primitive.
func FileSHA256(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(b)), nil
}

func (a *GcAdapter) buildEnv() []string {
	// PINNED, not passed through (2026-08-08 hunt, F1 — the worst class
	// of defect a conformance tool can have): HOME passthrough let
	// $HOME/.config/go/env silently set GOARCH/GOFLAGS for the
	// REFERENCE build while Identity() recorded the host defaults — a
	// user-level `go env -w GOARCH=386` fabricated observation-mismatch
	// verdicts against a correct clone, unfalsifiably. GOENV=off closes
	// the file; the explicit pins close the ambient-variable route; and
	// GOWORK=off keeps a go.work above the out dir from capturing the
	// build (F2 — the throwaway go.mod fences modules, not workspaces).
	env := []string{
		"GOTOOLCHAIN=local", "CGO_ENABLED=0",
		"GOENV=off", "GOWORK=off", "GOFLAGS=-buildvcs=false", "GOEXPERIMENT=",
		"GOOS=" + runtime.GOOS,
	}
	for _, key := range []string{"PATH", "HOME", "TMPDIR", "GOCACHE", "GOPATH", "GOMODCACHE"} {
		if v := os.Getenv(key); v != "" {
			env = append(env, key+"="+v)
		}
	}
	if a.GOARCH != "" {
		env = append(env, "GOARCH="+a.GOARCH)
	} else {
		env = append(env, "GOARCH="+runtime.GOARCH)
	}
	return env
}

func (a *GcAdapter) Run(ctx context.Context, caseDir string) Outcome {
	bin, err := a.resolveGo()
	if err != nil {
		return Outcome{Status: StatusAdapterErr, Detail: err.Error()}
	}
	// Build OUTPUT lives outside the case tree (E3): a batch is exactly
	// its manifested inputs, and the first judged pass previously left
	// case-<name>.bin beside them — which the descriptor validation now
	// correctly refuses as an unlisted file. Inputs stay pristine.
	exeDir, err := os.MkdirTemp("", "grossmith-bin-*")
	if err != nil {
		return Outcome{Status: StatusAdapterErr, Detail: err.Error()}
	}
	defer os.RemoveAll(exeDir)
	exe := filepath.Join(exeDir, "case-"+a.Name()+".bin")
	// -buildvcs=false (2026-08-08 review, G2): case binaries never need
	// VCS stamps, and Go 1.26's auto stamping runs git — which fails
	// (exit 128) under scratch HOMEs, foreign-owned checkouts, or
	// corrupted repos above the case dir, killing the build for reasons
	// that have nothing to do with the case.
	build := exec.CommandContext(ctx, bin, "build", "-buildvcs=false", "-o", exe, ".")
	build.Dir = caseDir
	build.Env = a.buildEnv()
	if out, err := build.CombinedOutput(); err != nil {
		return Outcome{Status: StatusBuildFailed, Detail: fmt.Sprintf("build: %v: %s", err, out)}
	}
	timeout := a.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	abs, err := filepath.Abs(exe)
	if err != nil {
		return Outcome{Status: StatusAdapterErr, Detail: err.Error()}
	}
	run := exec.CommandContext(runCtx, abs)
	run.Env = []string{}
	var stdout, stderr bytes.Buffer
	run.Stdout, run.Stderr = &stdout, &stderr
	err = run.Run()
	switch {
	case runCtx.Err() == context.DeadlineExceeded:
		return Outcome{Status: StatusTimeout, Detail: "timeout"}
	case err != nil:
		return Outcome{Status: StatusRunFailed, Detail: fmt.Sprintf("run: %v: %s", err, stderr.String())}
	}
	doc, err := observe.Parse(bytes.TrimSpace(stdout.Bytes()))
	if err != nil {
		return Outcome{Status: StatusRunFailed, Detail: "observation parse: " + err.Error()}
	}
	return Outcome{Status: StatusRan, Document: doc}
}

// RunBatch judges every case directory under root (sorted) with the
// reference and optional clone adapter.
func RunBatch(ctx context.Context, root string, ref Adapter, clone Adapter, policy observe.PanicPolicy, workers int) (BatchReport, error) {
	// The descriptor is authoritative (E3; audit P0: glob discovery
	// judged whatever was on disk — injected files included — and
	// verified nothing). Validation happens HERE, immediately before
	// execution: extra, missing, tampered, or symlinked inputs refuse
	// the whole batch.
	ids, err := ValidateBatch(root)
	if err != nil {
		return BatchReport{}, err
	}
	if len(ids) == 0 {
		return BatchReport{}, fmt.Errorf("no cases under %s", root)
	}
	dirs := make([]string, len(ids))
	for i, id := range ids {
		dirs[i] = filepath.Join(root, id)
	}

	rep := BatchReport{Schema: BatchSchema, PanicPolicy: string(policy),
		ReferenceName: ref.Name(), Started: time.Now().UTC().Format(time.RFC3339)}
	if rep.ReferenceIdentity, err = ref.Identity(ctx); err != nil {
		return BatchReport{}, fmt.Errorf("reference identity: %w", err)
	}
	if clone != nil {
		rep.CloneName = clone.Name()
		if rep.CloneIdentity, err = clone.Identity(ctx); err != nil {
			return BatchReport{}, fmt.Errorf("clone identity: %w", err)
		}
	}

	results := make([]CaseResult, len(dirs))
	if workers < 1 {
		workers = 1
	}
	var wg sync.WaitGroup
	jobs := make(chan int)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if ctx.Err() != nil {
					continue // drain the channel; the batch aborts below
				}
				dir := dirs[i]
				subject, err := os.ReadFile(filepath.Join(dir, "subject.go"))
				cr := CaseResult{ID: filepath.Base(dir)}
				if err != nil {
					cr.Verdict, cr.Detail = VerdictHarnessError, err.Error()
					results[i] = cr
					continue
				}
				cr.SubjectSHA256 = SubjectHash(subject)
				cr.Reference = ref.Run(ctx, dir)
				if clone != nil {
					co := clone.Run(ctx, dir)
					cr.Clone = &co
					cr.Verdict, cr.Detail = Judge(cr.Reference, co, policy)
				}
				results[i] = cr
			}
		}()
	}
	for i := range dirs {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	// An aborted batch is not a conformance statement (audit L5): without
	// this, cancellation surfaced as reference-infra-failure on every
	// remaining case in a report returned with a nil error.
	if err := ctx.Err(); err != nil {
		return BatchReport{}, fmt.Errorf("batch aborted: %w", err)
	}

	rep.Cases = results
	rep.Total = len(results)
	rep.Verdicts = map[Verdict]int{}
	sizeMin, sizeSum, sizeMax, sizeStats := 1<<31, 0, 0, 0
	for i, cr := range results {
		if cr.Verdict != "" {
			rep.Verdicts[cr.Verdict]++
		}
		if cr.Reference.Status == StatusRan {
			rep.RefRan++
			if cr.Reference.Document.Status == observe.StatusPanic {
				rep.PanicPaths++
			}
			for _, e := range cr.Reference.Document.Events {
				if e.At == "recovered" {
					// Count EVENTS, not cases (hunt F9: the old break
					// undercounted while the label said events).
					rep.Recovered++
				}
			}
		}
		if b, err := os.Stat(filepath.Join(dirs[i], "subject.go")); err == nil {
			n := int(b.Size())
			sizeSum += n
			sizeStats++
			if n < sizeMin {
				sizeMin = n
			}
			if n > sizeMax {
				sizeMax = n
			}
		}
	}
	if sizeStats > 0 {
		// The mean divides by SUCCESSFUL stats, not by Total (E1; audit
		// P2/P3: a failed stat silently deflated the mean). sizeMax == 0
		// means every stat failed; the 1<<31 min sentinel must not
		// escape into the artifact (hunt F11).
		rep.SubjectBytesMin, rep.SubjectBytesMean, rep.SubjectBytesMax = sizeMin, sizeSum/sizeStats, sizeMax
	}
	return rep, nil
}

// WriteBatch persists the report next to the cases.
func WriteBatch(root string, rep BatchReport) error {
	b, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, "batch.json"), b, 0o644)
}

// WriteCaseRecord persists one case's durable metadata.
func WriteCaseRecord(dir string, rec CaseRecord) error {
	rec.Schema = CaseSchema
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "case.json"), b, 0o644)
}
