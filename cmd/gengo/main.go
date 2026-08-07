// gengo generates a batch of small, outcome-deterministic Go programs and
// judges them against a clone through the harness verdict taxonomy.
//
//	gengo -n 1000 -seed 1 -out out                 # generate only
//	gengo -n 1000 -seed 1 -out out -judge          # + gc reference pass
//	gengo -n 1000 -seed 1 -out out -clone gc-386   # + degenerate clone (cross-arch)
//	gengo -n 1000 -seed 1 -out out -clone golean   # + GoLean campaign (deps/golean)
//
// The durable output of record is out/batch.json (grossmith-batch-v1);
// stdout is a view of it.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"grossmith/gen"
	"grossmith/golean"
	"grossmith/harness"
	"grossmith/observe"
)

type config struct {
	n       int
	seed    int64
	out     string
	swarm   bool
	stats   bool
	judge   bool
	clone   string // "", "gc-386", "golean", "golean:<checkout>"
	goBin   string
	policy  string
	// policySet: -panic-policy was given explicitly (vs defaulted) — the
	// golean campaign does not apply it, so an explicit value there is a
	// refused misconfiguration rather than a silently ignored flag.
	policySet bool
	timeout   time.Duration
	workers   int
	// replay: path to a case directory containing case.json; regenerate
	// the case from its record and verify byte and observation identity.
	replay string
	// pairs: pairwise-coverage mode (rung 5, GoLean R4) — generate this
	// many cases per PAIR of optional constructs, each pair force-included
	// into an otherwise ordinary swarm mix. Replaces -n.
	pairs int
	// explicit records which flags the user actually set (audit F5:
	// -replay must refuse generation flags rather than ignore them).
	explicit map[string]bool
}

func main() {
	var cfg config
	flag.IntVar(&cfg.n, "n", 100, "number of programs to generate")
	flag.Int64Var(&cfg.seed, "seed", 1, "base seed; case i uses seed+i")
	flag.StringVar(&cfg.out, "out", "out", "output directory (empty, or a previous gengo batch)")
	flag.BoolVar(&cfg.swarm, "swarm", true, "draw a per-seed construct mix")
	flag.BoolVar(&cfg.stats, "stats", false, "print the choice-frequency report (valid vs chosen per site)")
	flag.BoolVar(&cfg.judge, "judge", false, "run the gc reference pass over the batch")
	flag.StringVar(&cfg.clone, "clone", "", "clone to judge against: gc-386, golean, or golean:<checkout> (implies -judge)")
	flag.StringVar(&cfg.goBin, "go", "", "pinned go toolchain binary (default: resolve go from PATH)")
	flag.StringVar(&cfg.policy, "panic-policy", "exact", "panic equivalence: exact (message bytes) or kind (taxonomy only)")
	flag.DurationVar(&cfg.timeout, "timeout", 10*time.Second, "per-case run timeout")
	flag.IntVar(&cfg.workers, "workers", runtime.NumCPU(), "parallel build/run workers")
	flag.StringVar(&cfg.replay, "replay", "", "case directory to REPLAY from its case.json record (verifies byte + observation identity; ignores generation flags)")
	flag.IntVar(&cfg.pairs, "pairs", 0, "pairwise-coverage mode: generate this many cases per optional-construct PAIR (replaces -n)")
	flag.Parse()
	cfg.explicit = map[string]bool{}
	flag.Visit(func(f *flag.Flag) {
		cfg.explicit[f.Name] = true
		if f.Name == "panic-policy" {
			cfg.policySet = true
		}
	})

	if err := run(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "gengo:", err)
		os.Exit(1)
	}
}

// validate rejects a bad invocation BEFORE anything is written (audit H5):
// a refused run leaves the filesystem exactly as it found it.
func (c config) validate() (observe.PanicPolicy, string, error) {
	var policy observe.PanicPolicy
	switch {
	case c.pairs < 0:
		return "", "", fmt.Errorf("-pairs %d: must be non-negative", c.pairs)
	case c.pairs > 0 && c.explicit["n"]:
		return "", "", fmt.Errorf("-pairs replaces -n; give one or the other")
	case c.pairs == 0 && c.n < 1:
		return "", "", fmt.Errorf("-n %d: need at least one case", c.n)
	case c.out == "":
		return "", "", fmt.Errorf("-out: empty path")
	case c.timeout <= 0:
		return "", "", fmt.Errorf("-timeout %s: must be positive", c.timeout)
	case c.workers < 1:
		return "", "", fmt.Errorf("-workers %d: need at least one", c.workers)
	}
	switch c.policy {
	case "exact":
		policy = observe.PanicExact
	case "kind":
		policy = observe.PanicKindOnly
	default:
		return "", "", fmt.Errorf("-panic-policy %q: use exact or kind", c.policy)
	}
	checkout := ""
	switch {
	case c.clone == "" || c.clone == "gc-386":
	case c.clone == "golean":
		checkout = filepath.Join("deps", "golean")
	case strings.HasPrefix(c.clone, "golean:"):
		checkout = strings.TrimPrefix(c.clone, "golean:")
		if checkout == "" {
			return "", "", fmt.Errorf("-clone golean: empty checkout path")
		}
	default:
		return "", "", fmt.Errorf("-clone %q: use gc-386, golean, or golean:<checkout>", c.clone)
	}
	if checkout != "" {
		// Refuse BEFORE the generation and reference pass (audit F9): a
		// typo'd checkout or a run from outside the repo root previously
		// failed only after minutes of judged cases.
		if _, err := os.Stat(filepath.Join(checkout, "scripts", "diff-coverage")); err != nil {
			return "", "", fmt.Errorf("golean checkout: %w (run from the repo root, or pass -clone golean:<path>)", err)
		}
		// GoLean's harness applies its own panic equivalence (expected
		// status + exact panic message); -panic-policy would be recorded
		// but never used (audit F5) — refuse rather than misrecord.
		if c.policySet {
			return "", "", fmt.Errorf("-panic-policy is not applied by the golean campaign (GoLean pins exact panic messages); omit it")
		}
	}
	// The out dir must be ours: empty/absent, or a previous batch
	// (manifest.tsv present). Refusing foreign directories keeps the
	// stale-case cleanup from ever deleting someone else's files.
	entries, err := os.ReadDir(c.out)
	switch {
	case os.IsNotExist(err):
	case err != nil:
		return "", "", err
	case len(entries) > 0:
		if _, err := os.Stat(filepath.Join(c.out, "manifest.tsv")); err != nil {
			return "", "", fmt.Errorf("out dir %s is non-empty and not a gengo batch (no manifest.tsv) — refusing to touch it", c.out)
		}
	}
	return policy, checkout, nil
}

func run(cfg config) error {
	if cfg.replay != "" {
		return runReplay(cfg)
	}
	policy, checkout, err := cfg.validate()
	if err != nil {
		return err
	}
	judging := cfg.judge || cfg.clone != ""
	rev := generatorRev()

	if err := os.MkdirAll(cfg.out, 0o755); err != nil {
		return err
	}
	// The batch root is its own throwaway module so `go test ./...` in the
	// repo never vets generated programs (vet's style checks — redundant
	// `v || v` and friends — legitimately fire on random code).
	modfile := filepath.Join(cfg.out, "go.mod")
	if _, err := os.Stat(modfile); os.IsNotExist(err) {
		if err := os.WriteFile(modfile, []byte("module grossmith-cases\n\ngo 1.26\n"), 0o644); err != nil {
			return err
		}
	}
	// Mark the dir as ours IMMEDIATELY (audit F3): manifest.tsv is the
	// ownership token validate() checks, and writing it only after the full
	// generation loop meant an interrupted run left a dir every future run
	// refused. The header-only file is overwritten with the real manifest
	// below.
	if err := os.WriteFile(filepath.Join(cfg.out, "manifest.tsv"), []byte("id\tseed\tfeatures\tpair\n"), 0o644); err != nil {
		return err
	}
	// And a previous run's report must not survive next to regenerated
	// cases (audit F4): with index-based IDs a stale batch.json is
	// structurally consistent with the new dirs — only the subject hashes
	// disagree, and nothing rechecks them.
	if err := os.Remove(filepath.Join(cfg.out, "batch.json")); err != nil && !os.IsNotExist(err) {
		return err
	}

	// The case plan: plain (-n sequential seeds) or pairwise (rung 5,
	// GoLean R4 — every optional-construct pair force-included into an
	// otherwise ordinary swarm mix, cfg.pairs cases each).
	type caseSpec struct {
		seed    int64
		include []string
	}
	var specs []caseSpec
	if cfg.pairs > 0 {
		tags := gen.Optional()
		// The pair universe must respect the clone's capability profile
		// (audit F3: Include is applied after Exclude, so forcing an
		// excluded tag would hand the clone exactly the constructs its
		// profile removes and misattribute every resulting verdict).
		if checkout != "" {
			excluded := map[string]bool{}
			for _, t := range golean.Profile(gen.DefaultConfig(0)).Exclude {
				excluded[t] = true
			}
			kept := tags[:0:0]
			for _, t := range tags {
				if !excluded[t] {
					kept = append(kept, t)
				}
			}
			fmt.Printf("pairs mode: %d of %d tags in the pair universe (profile excludes %d)\n",
				len(kept), len(tags), len(tags)-len(kept))
			tags = kept
		}
		idx := 0
		for i := 0; i < len(tags); i++ {
			for j := i + 1; j < len(tags); j++ {
				for k := 0; k < cfg.pairs; k++ {
					specs = append(specs, caseSpec{cfg.seed + int64(idx), []string{tags[i], tags[j]}})
					idx++
				}
			}
		}
	} else {
		for i := 0; i < cfg.n; i++ {
			specs = append(specs, caseSpec{cfg.seed + int64(i), nil})
		}
	}

	tagCount := map[string]int{}
	siteStats := map[string]*gen.SiteStats{}
	featuresByID := map[string][]string{}
	liveDirs := map[string]bool{}
	var manifest strings.Builder
	manifest.WriteString("id\tseed\tfeatures\tpair\n")

	for i, spec := range specs {
		caseSeed := spec.seed
		gcfg := gen.DefaultConfig(caseSeed)
		gcfg.Swarm = cfg.swarm
		if checkout != "" {
			gcfg = golean.Profile(gcfg)
		}
		gcfg.Include = spec.include
		c, err := gen.New(gcfg).Generate()
		if err != nil {
			return fmt.Errorf("seed %d: %w", caseSeed, err)
		}
		id := fmt.Sprintf("case_%05d", i)
		dir := filepath.Join(cfg.out, id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "subject.go"), c.Source, 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "driver.go"), c.Driver, 0o644); err != nil {
			return err
		}
		if err := harness.WriteCaseRecord(dir, harness.CaseRecord{
			Schema: harness.CaseSchema, ID: id, Seed: caseSeed, GeneratorRev: rev,
			SubjectSHA256: harness.SubjectHash(c.Source),
			DriverSHA256:  harness.SubjectHash(c.Driver),
			Features:      c.FeatureCounts, DrawTrace: c.Tape,
			Config: gcfg,
		}); err != nil {
			return err
		}
		// Features carry per-program COUNTS (tag=N): presence saturates for
		// common tags; counts keep them stratifiable.
		counted := make([]string, len(c.Features))
		for fi, f := range c.Features {
			counted[fi] = fmt.Sprintf("%s=%d", f, c.FeatureCounts[f])
		}
		pairCol := "-"
		if len(spec.include) == 2 {
			pairCol = spec.include[0] + "+" + spec.include[1]
		}
		fmt.Fprintf(&manifest, "%s\t%d\t%s\t%s\n", id, caseSeed, strings.Join(counted, ","), pairCol)
		featuresByID[id] = c.Features
		liveDirs[dir] = true
		for _, t := range c.Features {
			tagCount[t]++
		}
		for site, s := range c.Stats {
			agg := siteStats[site]
			if agg == nil {
				agg = &gen.SiteStats{Valid: map[string]int{}, Chosen: map[string]int{}}
				siteStats[site] = agg
			}
			for k, v := range s.Valid {
				agg.Valid[k] += v
			}
			for k, v := range s.Chosen {
				agg.Chosen[k] += v
			}
		}
	}
	if err := os.WriteFile(filepath.Join(cfg.out, "manifest.tsv"), []byte(manifest.String()), 0o644); err != nil {
		return err
	}
	// Remove stale case dirs from a previous larger batch in the same out
	// dir: the batch glob would otherwise mix two batches' verdicts.
	stale, err := filepath.Glob(filepath.Join(cfg.out, "case_*"))
	if err != nil {
		return err
	}
	for _, dir := range stale {
		if !liveDirs[dir] {
			if err := os.RemoveAll(dir); err != nil {
				return err
			}
		}
	}
	fmt.Printf("generated %d cases in %s (seeds %d..%d)\n", len(specs), cfg.out, cfg.seed, cfg.seed+int64(len(specs))-1)
	if cfg.pairs > 0 {
		// Realized co-emission per forced pair: enabling a pair arms its
		// sites but emission depends on draws and legality — unrealized
		// pairs are the coverage signal, printed, never hidden (no silent
		// caps).
		perPair := map[string]int{}
		for i, spec := range specs {
			if len(spec.include) != 2 {
				continue
			}
			key := spec.include[0] + "+" + spec.include[1]
			if _, ok := perPair[key]; !ok {
				perPair[key] = 0
			}
			has := map[string]bool{}
			for _, f := range featuresByID[fmt.Sprintf("case_%05d", i)] {
				has[f] = true
			}
			if has[spec.include[0]] && has[spec.include[1]] {
				perPair[key]++
			}
		}
		realized, zeroPairs := 0, []string{}
		for key, n := range perPair {
			if n > 0 {
				realized++
			} else {
				zeroPairs = append(zeroPairs, key)
			}
		}
		sort.Strings(zeroPairs)
		fmt.Printf("\npair coverage: %d/%d pairs realized at least once\n", realized, len(perPair))
		for i, p := range zeroPairs {
			if i == 15 {
				fmt.Printf("  ... and %d more unrealized pairs\n", len(zeroPairs)-15)
				break
			}
			fmt.Printf("  unrealized: %s\n", p)
		}
	}

	fmt.Println("\ncomposition (tag histogram):")
	printHistogram(tagCount, len(specs))
	if cfg.stats {
		fmt.Println("\nchoice frequency (site/arm: valid -> chosen):")
		printSiteStats(siteStats)
	}
	if !judging {
		return nil
	}

	ctx := context.Background()
	ref := &harness.GcAdapter{GoBin: cfg.goBin, Timeout: cfg.timeout, AdapterName: "gc"}
	var cloneAd harness.Adapter
	if cfg.clone == "gc-386" {
		cloneAd = &harness.GcAdapter{GoBin: cfg.goBin, GOARCH: "386", Timeout: cfg.timeout, AdapterName: "gc-386"}
	}
	rep, err := harness.RunBatch(ctx, cfg.out, ref, cloneAd, policy, cfg.workers)
	if err != nil {
		return err
	}
	rep.GeneratorRev = rev
	rep.Seeds = [2]int64{cfg.seed, cfg.seed + int64(len(specs)) - 1}
	rep.Composition = tagCount
	// Wrapper catches (audit F2): a wrapped subject that caught a panic
	// returns status ok with a nonzero trailing code — invisible to
	// PanicPaths, counted here from features + reference documents.
	for _, cr := range rep.Cases {
		if !hasTag(featuresByID[cr.ID], "recover_wrapper") || cr.Reference.Status != harness.StatusRan {
			continue
		}
		doc := cr.Reference.Document
		if doc.Status == observe.StatusOK && len(doc.Values) > 0 {
			if last := doc.Values[len(doc.Values)-1]; last.Kind == "int" && last.Int != 0 {
				rep.WrapperCaught++
			}
		}
	}

	if checkout != "" {
		if err := runGoLean(ctx, &rep, cfg, checkout, featuresByID); err != nil {
			return err
		}
	}
	if err := harness.WriteBatch(cfg.out, rep); err != nil {
		return err
	}
	printReport(rep, cfg, featuresByID, tagCount)
	return nil
}

// caseRecordIn is CaseRecord with the config typed for reading back —
// the harness keeps it opaque, the CLI knows it is a gen.Config.
type caseRecordIn struct {
	Schema        string      `json:"schema"`
	ID            string      `json:"id"`
	Seed          int64       `json:"seed"`
	GeneratorRev  string      `json:"generatorRev"`
	SubjectSHA256 string      `json:"subjectSha256"`
	DriverSHA256  string      `json:"driverSha256"`
	DrawTrace     []int       `json:"drawTrace"`
	Config        *gen.Config `json:"config"`
}

// runReplay is Phase 3's done-when as a command: regenerate a case from
// its case.json record ALONE (no seed-range convention), verify the
// subject byte-identical by hash, re-run the reference, and — when the
// batch report is present next to the case — verify the observation
// document equal to the recorded one.
func runReplay(cfg config) error {
	// Refuse, never ignore, conflicting flags (audit F5 — the same shape
	// as the golean -panic-policy refusal: a flag that is recorded or
	// implied but unused misleads).
	for name := range cfg.explicit {
		switch name {
		case "replay", "go", "timeout":
		default:
			return fmt.Errorf("-%s does not apply to -replay (only -go and -timeout do)", name)
		}
	}
	if cfg.timeout <= 0 {
		return fmt.Errorf("-timeout %s: must be positive", cfg.timeout)
	}
	b, err := os.ReadFile(filepath.Join(cfg.replay, "case.json"))
	if err != nil {
		return err
	}
	var rec caseRecordIn
	if err := json.Unmarshal(b, &rec); err != nil {
		return fmt.Errorf("case.json: %w", err)
	}
	if rec.Schema != harness.CaseSchema {
		return fmt.Errorf("case.json: schema %q, want %q", rec.Schema, harness.CaseSchema)
	}
	if rec.Config == nil {
		// Fail closed with the real cause (audit F6: the zero config can
		// never validate, but the old error blamed a revision mismatch
		// while printing identical revisions).
		return fmt.Errorf("case.json has no resolved config — the record predates the config field (pre-Phase-2); regenerate the batch to make it replayable")
	}
	rcfg := *rec.Config
	rcfg.Seed = rec.Seed

	c, err := gen.NewReplay(rcfg, rec.DrawTrace).Generate()
	if err != nil {
		return fmt.Errorf("replay decode failed (recorded under generator %s, this binary is %s): %w",
			rec.GeneratorRev, generatorRev(), err)
	}
	if got := harness.SubjectHash(c.Source); got != rec.SubjectSHA256 {
		return fmt.Errorf("replayed subject hash %s != recorded %s (recorded under generator %s, this binary is %s)",
			got, rec.SubjectSHA256, rec.GeneratorRev, generatorRev())
	}
	fmt.Printf("replay %s: subject byte-identical (sha256 %s)\n", rec.ID, rec.SubjectSHA256[:12])
	// Driver identity too, when the record carries it (audit F10; older
	// records predate the field and skip with a note).
	if rec.DriverSHA256 == "" {
		fmt.Println("record has no driver hash (pre-F10 record) — driver verified via observation only")
	} else if got := harness.SubjectHash(c.Driver); got != rec.DriverSHA256 {
		return fmt.Errorf("replayed driver hash %s != recorded %s", got, rec.DriverSHA256)
	}

	dir, err := os.MkdirTemp("", "grossmith-replay-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	// The throwaway module, same as the batch path (audit F1: without it
	// the build only succeeds when the temp dir happens to sit inside a
	// module — this sandbox's TMPDIR did, every normal machine's does not).
	files := map[string][]byte{
		"subject.go": c.Source,
		"driver.go":  c.Driver,
		"go.mod":     []byte("module grossmith-cases\n\ngo 1.26\n"),
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			return err
		}
	}
	// The recorded outcome, read BEFORE the fresh run (audit F9: a case
	// that timed out or failed to build in the batch is exactly the one
	// worth replaying — a fresh failure matching the record is a
	// successful reproduction, not an error).
	var recorded *harness.CaseResult
	var refIdentity string
	batchPath := filepath.Join(filepath.Dir(filepath.Clean(cfg.replay)), "batch.json")
	if rb, err := os.ReadFile(batchPath); err == nil {
		var rep harness.BatchReport
		if err := json.Unmarshal(rb, &rep); err != nil {
			return fmt.Errorf("batch.json: %w", err)
		}
		refIdentity = rep.ReferenceIdentity
		for i := range rep.Cases {
			if rep.Cases[i].ID == rec.ID {
				recorded = &rep.Cases[i]
				break
			}
		}
		if recorded == nil {
			return fmt.Errorf("case %s not found in %s", rec.ID, batchPath)
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	ref := &harness.GcAdapter{GoBin: cfg.goBin, Timeout: cfg.timeout, AdapterName: "gc"}
	out := ref.Run(context.Background(), dir)
	if out.Status != harness.StatusRan {
		if recorded != nil && recorded.Reference.Status == out.Status {
			fmt.Printf("replay %s: outcome %s matches the record — source reproduced, no observation to compare\n", rec.ID, out.Status)
			return nil
		}
		return fmt.Errorf("replayed case did not run: %s: %s", out.Status, out.Detail)
	}
	fmt.Printf("replay %s: reference ran (status %s)\n", rec.ID, out.Document.Status)

	switch {
	case recorded == nil:
		fmt.Println("no batch.json next to the case — observation verified against a fresh run only")
	case recorded.Reference.Status != harness.StatusRan:
		fmt.Printf("recorded reference outcome was %s but the fresh run ran — environment change, source still byte-identical\n", recorded.Reference.Status)
	default:
		// gc-vs-gc: exact panic identity is the right (strictest) policy
		// regardless of what the batch was judged under.
		eq, err := observe.Equal(out.Document, recorded.Reference.Document, observe.PanicExact)
		if err != nil {
			return err
		}
		if !eq {
			return fmt.Errorf("replayed observation differs from the recorded one (reference identity then: %s)", refIdentity)
		}
		fmt.Printf("replay %s: observation byte-identical to the batch record\n", rec.ID)
	}
	return nil
}

// runGoLean judges the batch's reference outcomes through GoLean's harness
// and folds the verdicts into the report.
func runGoLean(ctx context.Context, rep *harness.BatchReport, cfg config, checkout string, featuresByID map[string][]string) error {
	identity, err := golean.Identity(ctx, checkout)
	if err != nil {
		return err
	}
	rep.CloneName, rep.CloneIdentity = "golean", identity
	// The record states the equivalence actually applied — GoLean's, not
	// the -panic-policy default (audit F5).
	rep.PanicPolicy = "golean-harness (expected status + exact panic message)"
	cases := make([]golean.Case, len(rep.Cases))
	for i, cr := range rep.Cases {
		cases[i] = golean.Case{ID: cr.ID, Dir: filepath.Join(cfg.out, cr.ID),
			Features: featuresByID[cr.ID], Reference: cr.Reference}
	}
	results, err := golean.Run(ctx, filepath.Join(cfg.out, "golean-work"), cases, golean.Config{
		Checkout: checkout, Jobs: cfg.workers,
	})
	if err != nil {
		return err
	}
	rep.Verdicts = map[harness.Verdict]int{}
	for i := range rep.Cases {
		res, ok := results[rep.Cases[i].ID]
		if !ok {
			return fmt.Errorf("golean returned no verdict for %s", rep.Cases[i].ID)
		}
		rep.Cases[i].Verdict = res.Verdict
		rep.Cases[i].Detail = res.Detail
		if res.Stage != "" {
			rep.Cases[i].Detail = "stage " + res.Stage + ": " + res.Detail
		}
		rep.Verdicts[res.Verdict]++
	}
	return nil
}

func printReport(rep harness.BatchReport, cfg config, featuresByID map[string][]string, tagCount map[string]int) {
	fmt.Printf("\nconformance statement (out/batch.json is the record):\n")
	fmt.Printf("  reference: %s\n", rep.ReferenceIdentity)
	if rep.CloneName != "" {
		fmt.Printf("  clone:     %s (%s)\n", rep.CloneName, rep.CloneIdentity)
	}
	fmt.Printf("  policy: panic-%s   cases: %d   ref-ran: %d   panic-paths: %d   recovered-events: %d   wrapper-caught: %d\n",
		rep.PanicPolicy, rep.Total, rep.RefRan, rep.PanicPaths, rep.Recovered, rep.WrapperCaught)
	fmt.Printf("  subject bytes min/mean/max: %d/%d/%d\n",
		rep.SubjectBytesMin, rep.SubjectBytesMean, rep.SubjectBytesMax)
	if len(rep.Verdicts) > 0 {
		fmt.Println("  verdicts:")
		keys := make([]string, 0, len(rep.Verdicts))
		for v := range rep.Verdicts {
			keys = append(keys, string(v))
		}
		sort.Strings(keys)
		for _, v := range keys {
			fmt.Printf("    %-24s %5d\n", v, rep.Verdicts[harness.Verdict(v)])
		}
	}
	shown := 0
	for _, cr := range rep.Cases {
		// Every non-match is worth a line (audit F10: infra failures were
		// visible only as a count, hiding e.g. WHICH construct a clone's
		// frontend lacks).
		interesting := (cr.Verdict != "" && cr.Verdict != harness.VerdictMatch) ||
			(cr.Verdict == "" && cr.Reference.Status != harness.StatusRan)
		if !interesting {
			continue
		}
		if shown == 8 {
			fmt.Println("  ...")
			break
		}
		detail := cr.Detail
		if detail == "" {
			detail = string(cr.Reference.Status) + ": " + cr.Reference.Detail
		}
		fmt.Printf("  %s %s: %s\n", cr.ID, cr.Verdict, firstLine(detail))
		shown++
	}
	// Cross-arch discrimination: divergences must fall inside the declared
	// width_dependent quotient (tag honesty).
	if cfg.clone == "gc-386" {
		inTag, offTag := 0, 0
		for _, cr := range rep.Cases {
			if cr.Verdict != harness.VerdictMismatch {
				continue
			}
			if hasTag(featuresByID[cr.ID], "width_dependent") {
				inTag++
			} else {
				offTag++
				fmt.Printf("  UNTAGGED divergence in %s (tag honesty violation)\n", cr.ID)
			}
		}
		widthTagged := tagCount["width_dependent"]
		fmt.Printf("\ncross-arch discrimination: divergences in-tag %d, off-tag %d; width_dependent-tagged %d\n",
			inTag, offTag, widthTagged)
		if widthTagged > 0 {
			fmt.Printf("  width_dependent yield: %d/%d (%.1f%%)\n",
				inTag, widthTagged, 100*float64(inTag)/float64(widthTagged))
		}
	}
}

// generatorRev is the generator's own identity in every artifact: VCS
// revision from build info, "-dirty" when the build had local changes.
// `go run` and test builds do not stamp VCS settings (audit M5 observed
// "unknown" in real artifacts), so the fallback asks git about the working
// directory — prefixed so the record is honest about its provenance.
func generatorRev() string {
	rev, dirty := "", false
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				rev = s.Value
			case "vcs.modified":
				dirty = s.Value == "true"
			}
		}
	}
	if rev != "" {
		if dirty {
			rev += "-dirty"
		}
		return rev
	}
	out, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	rev = "cwd-git:" + strings.TrimSpace(string(out))
	if s, err := exec.Command("git", "status", "--porcelain").Output(); err == nil && len(strings.TrimSpace(string(s))) > 0 {
		rev += "-dirty"
	}
	return rev
}

func hasTag(tags []string, want string) bool {
	for _, t := range tags {
		if t == want {
			return true
		}
	}
	return false
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func printHistogram(count map[string]int, total int) {
	keys := make([]string, 0, len(count))
	for k := range count {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if count[keys[i]] != count[keys[j]] {
			return count[keys[i]] > count[keys[j]]
		}
		return keys[i] < keys[j]
	})
	for _, k := range keys {
		fmt.Printf("  %-18s %5d  (%.0f%%)\n", k, count[k], 100*float64(count[k])/float64(total))
	}
}

func printSiteStats(stats map[string]*gen.SiteStats) {
	sites := make([]string, 0, len(stats))
	for s := range stats {
		sites = append(sites, s)
	}
	sort.Strings(sites)
	for _, site := range sites {
		s := stats[site]
		arms := make([]string, 0, len(s.Valid))
		for a := range s.Valid {
			arms = append(arms, a)
		}
		sort.Strings(arms)
		fmt.Printf("  %s:\n", site)
		for _, a := range arms {
			fmt.Printf("    %-14s %7d -> %7d\n", a, s.Valid[a], s.Chosen[a])
		}
	}
}
