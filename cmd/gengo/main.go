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
	"crypto/sha256"
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
	// verify: path to a PUBLISHED batch; run the full descriptor
	// verification offline (mid-arc review finding 7: the descriptor's
	// guarantee was unreachable outside the producing process).
	verify string
	// pairs: pairwise-coverage mode (rung 5, GoLean R4) — generate this
	// many cases per PAIR of optional constructs, each pair force-included
	// into an otherwise ordinary swarm mix. Replaces -n.
	pairs int
	// allowDirty: permit a judged campaign from a dirty generator/clone
	// tree (E3; audit P1: "-dirty" collapses arbitrary changes to one
	// label). The dirty tree's content hash is recorded instead.
	allowDirty bool
	// allowLegacyVerify: let -verify exit 0 on a batch whose descriptors
	// predate the E5 bindings (no reportFiles, no clone digests). The
	// DEFAULT is a refusal: those fields are also absent after a
	// hand-edit removed them, and an exit-0 with a reduced-scope print
	// is exactly what a script consumer would miss (E5 re-review).
	allowLegacyVerify bool
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
	flag.StringVar(&cfg.verify, "verify", "", "batch directory to VERIFY offline against its descriptor (complete.json required; no generation)")
	flag.IntVar(&cfg.pairs, "pairs", 0, "pairwise-coverage mode: generate this many cases per optional-construct PAIR (replaces -n)")
	flag.BoolVar(&cfg.allowDirty, "allow-dirty", false, "permit judged campaigns from a dirty tree (records a content hash instead of refusing)")
	flag.BoolVar(&cfg.allowLegacyVerify, "allow-legacy-verify", false, "let -verify accept a pre-E5 batch whose report/clone digests are absent (default: refuse the reduced scope)")
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

// stageBatchDir prepares the staging sibling `<out>.staging`: leftover
// staging from an interrupted run is removed (it is ours by naming and
// was never published), and an interrupted PUBLISH — `<out>.prev`
// present with `<out>` missing — is rolled back first, restoring the
// previous valid batch (E3: interruption preserves the previous batch).
func stageBatchDir(out string) (string, error) {
	prev := out + ".prev"
	if _, err := os.Stat(prev); err == nil {
		// The leftover must be OURS before recovery touches it (mid-arc
		// review finding 2: an unowned <out>.prev holding user data was
		// renamed over out — logged as a recovery — then consumed and
		// deleted; an unowned <out>.staging was deleted outright). A
		// prev only ever comes from publishBatch renaming a PUBLISHED
		// batch, so the proof is a manifest that actually PARSES as our
		// descriptor — not the mere presence of a file with the right
		// name (E5, the finding's recorded residual: a directory holding
		// any file called manifest.json was recovered and consumed).
		if !ownedBatchDir(prev) {
			return "", fmt.Errorf("%s exists and is not a gengo batch — refusing to touch it (move it aside to proceed)", prev)
		}
		if _, err := os.Stat(out); os.IsNotExist(err) {
			if err := os.Rename(prev, out); err != nil {
				return "", fmt.Errorf("recovering interrupted publish: %w", err)
			}
			fmt.Fprintf(os.Stderr, "gengo: recovered interrupted publish (%s restored from %s)\n", out, prev)
		} else {
			// Both exist: the crash happened after the new batch was
			// published but before cleanup — the leftover is disposable.
			if err := os.RemoveAll(prev); err != nil {
				return "", err
			}
		}
	}
	staging := out + ".staging"
	if fi, err := os.Lstat(staging); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 || !fi.IsDir() || !stagingIsOurs(staging, out) {
			return "", fmt.Errorf("%s exists and is not this out dir's gengo staging tree — refusing to touch it (move it aside to proceed)", staging)
		}
		if err := os.RemoveAll(staging); err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return "", err
	}
	// The ownership marker is the FIRST write: a crash at any later
	// point leaves a tree the next run can prove is its own leftover.
	// Its CONTENT binds the out dir (E5: a filename alone proved only
	// that something once wrote a marker, not that this tree is THIS
	// batch's staging).
	if err := os.WriteFile(filepath.Join(staging, harness.StagingMarker()), []byte(stagingMarkerContent(out)), 0o644); err != nil {
		return "", err
	}
	return staging, nil
}

func stagingMarkerContent(out string) string {
	abs, err := filepath.Abs(out)
	if err != nil {
		abs = out
	}
	return "gengo staging for " + abs + " — safe to delete\n"
}

// ownedBatchDir: the tree parses as a published gengo batch. A CONTENT
// test, not a filename test — ReadManifest checks the schema and every
// digest's shape, which nothing but gengo writes.
func ownedBatchDir(dir string) bool {
	_, err := harness.ReadManifest(dir)
	return err == nil
}

// stagingIsOurs: the marker exists AND names this out dir.
func stagingIsOurs(dir, out string) bool {
	b, err := os.ReadFile(filepath.Join(dir, harness.StagingMarker()))
	return err == nil && string(b) == stagingMarkerContent(out)
}

// writeComplete writes the completion descriptor LAST: the manifest's
// digest plus the report artifacts' digests (E5, review B1 — verdicts
// live in batch.json, so an unbound report was an unbound conclusion).
// Consumers treat a tree without it as an interrupted run.
func writeComplete(work string) error {
	return harness.WriteComplete(work)
}

// publishBatch atomically swaps staging into place. The previous batch
// survives as `<out>.prev` until the swap succeeds; stageBatchDir
// recovers the one crash window (prev present, out missing) on the next
// run.
func publishBatch(out, work string) error {
	prev := out + ".prev"
	if _, err := os.Stat(out); err == nil {
		if err := os.Rename(out, prev); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(work, out); err != nil {
		return err
	}
	return os.RemoveAll(prev)
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
		// git is needed for the clone identity — refuse now, not after the
		// full reference pass (hunt F5: F9's guarantee was incomplete).
		if _, err := exec.LookPath("git"); err != nil {
			return "", "", fmt.Errorf("golean campaigns need git for the clone identity: %w", err)
		}
		// GoLean's harness applies its own panic equivalence (expected
		// status + exact panic message); -panic-policy would be recorded
		// but never used (audit F5) — refuse rather than misrecord.
		if c.policySet {
			return "", "", fmt.Errorf("-panic-policy is not applied by the golean campaign (GoLean pins exact panic messages); omit it")
		}
	}
	// The out dir must be ours: empty/absent, or a previous batch
	// (manifest present). Refusing foreign directories keeps publishBatch
	// from ever renaming away someone else's files (E3: publish replaces
	// the whole dir, so the ownership bar is load-bearing).
	entries, err := os.ReadDir(c.out)
	switch {
	case os.IsNotExist(err):
	case err != nil:
		return "", "", err
	case len(entries) > 0:
		if _, err := os.Stat(filepath.Join(c.out, "manifest.json")); err != nil {
			if _, err := os.Stat(filepath.Join(c.out, "manifest.tsv")); err != nil {
				return "", "", fmt.Errorf("out dir %s is non-empty and not a gengo batch (no manifest) — refusing to touch it", c.out)
			}
		}
	}
	return policy, checkout, nil
}

func run(cfg config) error {
	if cfg.verify != "" {
		for name := range cfg.explicit {
			if name != "verify" && name != "allow-legacy-verify" {
				return fmt.Errorf("-%s does not apply to -verify", name)
			}
		}
		info, err := harness.VerifyBatch(cfg.verify)
		if err != nil {
			return err
		}
		fmt.Printf("batch %s verified: %d cases, every case-input digest intact and bound to the manifest\n", cfg.verify, len(info.IDs))
		if info.ReportsBound {
			fmt.Printf("  report bound: batch.json/manifest.tsv digests match complete.json\n")
		} else if cfg.allowLegacyVerify {
			// reportFiles absent: written before the binding existed, or
			// removed by a later hand-edit — indistinguishable offline.
			// Only the explicit flag turns that into a pass, and the
			// reduced scope is stated either way (review B1's lesson).
			fmt.Printf("  report NOT bound (-allow-legacy-verify): reportFiles absent — batch.json and manifest.tsv are unchecked\n")
		} else {
			return fmt.Errorf("complete.json carries no reportFiles, so batch.json and manifest.tsv are unchecked — a pre-E5 batch or a later edit; pass -allow-legacy-verify to accept the reduced scope")
		}
		// The clone's side (E5, review B2): the work tree the clone
		// compiled from, checked against the report's recorded digests.
		workDir := filepath.Join(cfg.verify, "golean-work")
		batchPath := filepath.Join(cfg.verify, "batch.json")
		switch rb, err := os.ReadFile(batchPath); {
		case os.IsNotExist(err):
			fmt.Printf("  no batch.json: an unjudged batch — nothing further to bind\n")
		case err != nil:
			return err
		default:
			var rep harness.BatchReport
			if err := json.Unmarshal(rb, &rep); err != nil {
				return fmt.Errorf("batch.json: %w", err)
			}
			cloneRecorded := rep.CloneWorkFiles != nil
			for _, cr := range rep.Cases {
				if cr.CloneSourceSHA256 != "" {
					cloneRecorded = true
					break
				}
			}
			switch {
			case cloneRecorded:
				if err := golean.VerifyWork(workDir, rep); err != nil {
					return err
				}
				fmt.Printf("  clone tree bound: every recorded clone source and work file matches golean-work/\n")
			case rep.CloneName != "" && cfg.allowLegacyVerify:
				fmt.Printf("  clone tree NOT bound (-allow-legacy-verify): no clone digests in the report — golean-work/ is unchecked\n")
			case rep.CloneName != "":
				return fmt.Errorf("the report names clone %s but carries no clone source digests, so golean-work/ is unchecked — a pre-E5 batch or a later edit; pass -allow-legacy-verify to accept the reduced scope", rep.CloneName)
			default:
				fmt.Printf("  no clone in this batch\n")
			}
		}
		return nil
	}
	if cfg.replay != "" {
		return runReplay(cfg)
	}
	policy, checkout, err := cfg.validate()
	if err != nil {
		return err
	}
	judging := cfg.judge || cfg.clone != ""
	rev := generatorRev()
	if judging && strings.HasSuffix(rev, "-dirty") {
		// A campaign of record from a dirty tree records an identity that
		// names no reviewable revision (E3). Refuse, or — under
		// -allow-dirty — bind the actual content: HEAD-relative diff plus
		// the untracked file list, hashed.
		if !cfg.allowDirty {
			return fmt.Errorf("judged campaign from a dirty generator tree (%s): commit first, or pass -allow-dirty to record a content hash", rev)
		}
		if h, err := dirtyContentHash("."); err == nil {
			rev += "+content-" + h[:12]
		} else {
			return fmt.Errorf("-allow-dirty: hashing the dirty tree: %w", err)
		}
	}

	// Preflight the toolchain BEFORE any write (E1; audit: a bad -go was
	// discovered only after the batch existed, contradicting "validates
	// every input before writing anything"), and resolve it ONCE to an
	// absolute binary (E2; audit P0: the clone's nested oracle resolved
	// `go` from ambient PATH — a different Go could do the value
	// comparison than the one the report named). Everything downstream —
	// both GcAdapters and the GoLean script shim — uses this one path.
	var refOracle *harness.OracleIdentity
	if judging || cfg.goBin != "" {
		probe := &harness.GcAdapter{GoBin: cfg.goBin}
		oid, err := probe.Oracle(context.Background())
		if err != nil {
			return fmt.Errorf("go toolchain preflight (-go %q): %w", cfg.goBin, err)
		}
		cfg.goBin = oid.Path
		refOracle = &oid
	}

	// Batches are IMMUTABLE runs built in a STAGING sibling and published
	// by atomic rename (E3; audit P1: in-place regeneration truncated the
	// manifest first, deleted batch.json, and overwrote cases one at a
	// time — an interruption left a hybrid that still passed the
	// ownership check; a symlinked case dir let writes escape the tree).
	// Everything below — generation, judging, artifacts — lands in
	// `work`; cfg.out is touched only inside publishBatch. This deletes
	// the whole in-place mutation class: no ownership token, no stale-dir
	// sweep, no batch.json removal.
	work, err := stageBatchDir(cfg.out)
	if err != nil {
		return err
	}
	// The batch root is its own throwaway module so `go test ./...` in the
	// repo never vets generated programs (vet's style checks — redundant
	// `v || v` and friends — legitimately fire on random code). Staging is
	// fresh, so it is always written, never inherited (audit P0: a reused
	// root's go.mod kept arbitrary module/toolchain/replace directives).
	if err := os.WriteFile(filepath.Join(work, "go.mod"), []byte("module grossmith-cases\n\ngo 1.26\n"), 0o644); err != nil {
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
	var caseIDs []string
	caseSeeds := map[string]int64{}
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
		dir := filepath.Join(work, id)
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
		caseIDs = append(caseIDs, id)
		caseSeeds[id] = caseSeed
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
	if err := os.WriteFile(filepath.Join(work, "manifest.tsv"), []byte(manifest.String()), 0o644); err != nil {
		return err
	}
	// The AUTHORITATIVE descriptor (E3): every build input digested;
	// RunBatch refuses the tree if anything differs at judge time.
	if _, err := harness.WriteManifest(work, rev, "go 1.26", caseIDs, caseSeeds); err != nil {
		return err
	}
	fmt.Printf("generated %d cases (seeds %d..%d)\n", len(specs), cfg.seed, cfg.seed+int64(len(specs))-1)
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
		if err := writeComplete(work); err != nil {
			return err
		}
		return publishBatch(cfg.out, work)
	}

	ctx := context.Background()
	ref := &harness.GcAdapter{GoBin: cfg.goBin, Timeout: cfg.timeout, AdapterName: "gc"}
	var cloneAd harness.Adapter
	if cfg.clone == "gc-386" {
		cloneAd = &harness.GcAdapter{GoBin: cfg.goBin, GOARCH: "386", Timeout: cfg.timeout, AdapterName: "gc-386"}
	}
	rep, err := harness.RunBatch(ctx, work, ref, cloneAd, policy, cfg.workers)
	if err != nil {
		return err
	}
	rep.GeneratorRev = rev
	rep.Seeds = [2]int64{cfg.seed, cfg.seed + int64(len(specs)) - 1}
	rep.Composition = tagCount
	rep.ReferenceOracle = refOracle
	if rep.Budgets != nil {
		rep.Budgets.RunTimeout = cfg.timeout.String()
	}

	if checkout != "" {
		if err := runGoLean(ctx, &rep, cfg, work, checkout, featuresByID); err != nil {
			return err
		}
	}
	// Wrapper catches, counted AFTER the clone verdicts exist (the first
	// draft ran before runGoLean and judged against empty verdicts —
	// exactly the generated-vs-judged conflation G3 warns about, in the
	// metric meant to fix it).
	judgedWrapped, cloneInfraWrapped := 0, 0
	for _, cr := range rep.Cases {
		if !hasTag(featuresByID[cr.ID], "recover_wrapper") || cr.Reference.Status != harness.StatusRan {
			continue
		}
		doc := cr.Reference.Document
		if doc.Status == observe.StatusOK && len(doc.Values) > 0 {
			if last := doc.Values[len(doc.Values)-1]; last.Kind == "int" && last.Int != 0 {
				rep.WrapperCaught++
				if cr.Verdict == harness.VerdictMatch || cr.Verdict == harness.VerdictMismatch {
					judgedWrapped++
				}
				if cr.Verdict == harness.VerdictCloneInfra {
					cloneInfraWrapped++
				}
			}
		}
	}
	if rep.CloneName != "" {
		// Judged is meaningful only when a clone judged; null in the
		// artifact otherwise (hunt F6: an unconditional zero reproduced
		// the exact caught-without-judged signature on clone-less runs).
		// CloneInfra is the third leg (mid-arc review finding 1): the
		// nightly gates on caught == judged + cloneInfra exactly, so a
		// judgement regression cannot hide in unrelated quarantine slack.
		rep.WrapperJudged = &judgedWrapped
		rep.WrapperCloneInfra = &cloneInfraWrapped
		judgedComp := map[string]int{}
		for _, cr := range rep.Cases {
			if cr.Verdict != harness.VerdictMatch && cr.Verdict != harness.VerdictMismatch {
				continue
			}
			for _, tag := range featuresByID[cr.ID] {
				judgedComp[tag]++
			}
		}
		rep.CompositionJudged = judgedComp
	}
	rep.Pairs = cfg.pairs
	if err := harness.WriteBatch(work, rep); err != nil {
		return err
	}
	// Completion descriptor LAST, then atomic publish: a tree without
	// complete.json is an interrupted run, never a batch of record.
	if err := writeComplete(work); err != nil {
		return err
	}
	if err := publishBatch(cfg.out, work); err != nil {
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
	SubjectSHA256 string         `json:"subjectSha256"`
	DriverSHA256  string         `json:"driverSha256"`
	Features      map[string]int `json:"features"`
	DrawTrace     []int          `json:"drawTrace"`
	Config        *gen.Config    `json:"config"`
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
	dec := json.NewDecoder(strings.NewReader(string(b)))
	// Strict decode (E3; audit P1: unknown record fields passed through
	// silently — a record from a future or foreign producer verified).
	dec.DisallowUnknownFields()
	if err := dec.Decode(&rec); err != nil {
		return fmt.Errorf("case.json: %w", err)
	}
	if rec.Schema != harness.CaseSchema {
		return fmt.Errorf("case.json: schema %q, want %q", rec.Schema, harness.CaseSchema)
	}
	// The directory IS the ID: a record copied into another case's dir
	// would otherwise verify under the wrong identity (E3).
	if base := filepath.Base(filepath.Clean(cfg.replay)); base != rec.ID {
		return fmt.Errorf("case directory %q does not match the record's ID %q", base, rec.ID)
	}
	if rec.Config == nil {
		// Fail closed with the real cause (audit F6: the zero config can
		// never validate, but the old error blamed a revision mismatch
		// while printing identical revisions).
		return fmt.Errorf("case.json has no resolved config — the record predates the config field (pre-Phase-2); regenerate the batch to make it replayable")
	}
	rcfg := *rec.Config
	rcfg.Seed = rec.Seed
	if rec.GeneratorRev != generatorRev() {
		// Replay is a SAME-REVISION contract (the replay design): under a
		// different generator, decode failures and byte/feature
		// mismatches below are expected VERSION SKEW, not corruption —
		// say so up front instead of letting the mismatch errors imply
		// bad metadata (mid-arc review finding 13).
		fmt.Fprintf(os.Stderr, "gengo: WARNING: record from generator %s, this binary is %s — replay is same-revision; any mismatch below is version skew, not necessarily corruption\n",
			rec.GeneratorRev, generatorRev())
	}

	c, err := gen.NewReplay(rcfg, rec.DrawTrace).Generate()
	if err != nil {
		return fmt.Errorf("replay decode failed (recorded under generator %s, this binary is %s): %w",
			rec.GeneratorRev, generatorRev(), err)
	}
	if got := harness.SubjectHash(c.Source); got != rec.SubjectSHA256 {
		return fmt.Errorf("replayed subject hash %s != recorded %s (recorded under generator %s, this binary is %s)",
			got, rec.SubjectSHA256, rec.GeneratorRev, generatorRev())
	}
	// Feature metadata is part of the experiment, not decoration: it
	// drives attribution, profiles, and width claims (E3; audit P1 —
	// replay verified source bytes better than semantics).
	if len(rec.Features) > 0 {
		for tag, n := range c.FeatureCounts {
			if rec.Features[tag] != n {
				return fmt.Errorf("replayed feature %s=%d != recorded %d — the record's metadata does not describe this program", tag, n, rec.Features[tag])
			}
		}
		for tag, n := range rec.Features {
			if c.FeatureCounts[tag] != n {
				return fmt.Errorf("recorded feature %s=%d never regenerated", tag, n)
			}
		}
	}
	fmt.Printf("replay %s: subject byte-identical (sha256 %s), features identical (%d tags)\n", rec.ID, harness.ShortDigest(rec.SubjectSHA256), len(rec.Features))
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
			fmt.Printf("replay %s: SAME FAILURE CLASS as the record (%s) — source reproduced; a class match is not observation identity (E3)\n", rec.ID, out.Status)
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
func runGoLean(ctx context.Context, rep *harness.BatchReport, cfg config, work string, checkout string, featuresByID map[string][]string) error {
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
		cases[i] = golean.Case{ID: cr.ID, Dir: filepath.Join(work, cr.ID),
			Features: featuresByID[cr.ID], Reference: cr.Reference}
	}
	// The nested oracle is the SAME pinned binary, in their module mode,
	// via the PATH shim; the script's own hash makes the oracle pair
	// checkable offline (E2).
	if rep.ReferenceOracle != nil {
		nested := *rep.ReferenceOracle
		nested.ModuleMode = "GOPATH (GO111MODULE=off)"
		scriptSum, err := harness.FileSHA256(filepath.Join(checkout, "scripts", "diff-coverage"))
		if err != nil {
			return fmt.Errorf("hashing the clone oracle script: %w", err)
		}
		nested.ScriptSHA256 = scriptSum
		rep.CloneNestedOracle = &nested
	}
	results, err := golean.Run(ctx, filepath.Join(work, "golean-work"), cases, golean.Config{
		Checkout: checkout, Jobs: cfg.workers, GoBin: cfg.goBin,
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
	// Digest the clone's work tree into the report (E5, review B2): the
	// source the clone compiled per case, plus the run artifacts. The
	// translated main.go is a byte copy of subject.go, so a differing
	// digest here means the tree changed under the run — refuse the
	// batch rather than record a claim the tree contradicts.
	perCase, workFiles, err := golean.WorkDigests(filepath.Join(work, "golean-work"))
	if err != nil {
		return err
	}
	for i := range rep.Cases {
		sum, ok := perCase[rep.Cases[i].ID]
		if !ok {
			continue // never translated (refused before the clone)
		}
		if sum != rep.Cases[i].SubjectSHA256 {
			return fmt.Errorf("golean work: case %s main.go digest %s differs from the subject's %s — the clone's tree changed during the run",
				rep.Cases[i].ID, harness.ShortDigest(sum), harness.ShortDigest(rep.Cases[i].SubjectSHA256))
		}
		rep.Cases[i].CloneSourceSHA256 = sum
		delete(perCase, rep.Cases[i].ID)
	}
	for id := range perCase {
		return fmt.Errorf("golean work: translated case %s is not in the report", id)
	}
	rep.CloneWorkFiles = workFiles
	// The clone-side budgets that governed this campaign, in the record
	// (E5: the log cap was an inline literal and the lake budget — the
	// largest in the system — was unrecorded).
	if rep.Budgets != nil {
		rep.Budgets.CloneLogCap = golean.LogCap
		rep.Budgets.LakeBuildTimeout = golean.DefaultLakeBuildTimeout.String()
		rep.Budgets.CloneRunCeiling = golean.RunCeiling(golean.DefaultLakeBuildTimeout, len(cases)).String()
	}
	return nil
}

func printReport(rep harness.BatchReport, cfg config, featuresByID map[string][]string, tagCount map[string]int) {
	fmt.Printf("\nconformance statement (%s is the record):\n", filepath.Join(cfg.out, "batch.json"))
	fmt.Printf("  reference: %s\n", rep.ReferenceIdentity)
	if rep.CloneName != "" {
		fmt.Printf("  clone:     %s (%s)\n", rep.CloneName, rep.CloneIdentity)
	}
	judgedStr := "n/a (no clone)"
	if rep.WrapperJudged != nil {
		judgedStr = fmt.Sprintf("%d", *rep.WrapperJudged)
	}
	fmt.Printf("  policy: panic-%s   cases: %d   ref-ran: %d   panic-paths: %d   recovered-events: %d   wrapper-caught: %d   wrapper-JUDGED: %s\n",
		rep.PanicPolicy, rep.Total, rep.RefRan, rep.PanicPaths, rep.Recovered, rep.WrapperCaught, judgedStr)
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
		// The denominator is JUDGED cases (E1; audit: an all-infra batch
		// printed a reassuring zero/zero). Zero judged is an incomplete
		// campaign, said so.
		judged := 0
		for _, cr := range rep.Cases {
			if cr.Verdict == harness.VerdictMatch || cr.Verdict == harness.VerdictMismatch {
				judged++
			}
		}
		widthTagged := tagCount["width_dependent"]
		if judged == 0 {
			fmt.Printf("\ncross-arch discrimination: INCOMPLETE CAMPAIGN — 0 of %d cases reached a semantic verdict; the in-tag/off-tag counts below are vacuous\n", rep.Total)
		}
		fmt.Printf("\ncross-arch discrimination: divergences in-tag %d, off-tag %d over %d judged cases; width_dependent-tagged %d\n",
			inTag, offTag, judged, widthTagged)
		if widthTagged > 0 && judged > 0 {
			fmt.Printf("  width_dependent yield: %d/%d (%.1f%%)\n",
				inTag, widthTagged, 100*float64(inTag)/float64(widthTagged))
		}
	}
}

// gitProbe runs one identity/dirtiness query under its own budget and
// group cancellation with the sanitized env (E5, review C1 sweep: these
// were bare exec.Command — a git waiting on a lock or filesystem monitor
// stalled the run before any case existed).
func gitProbe(args ...string) ([]byte, error) {
	gitEnv := []string{}
	for _, key := range []string{"PATH", "HOME"} {
		if v := os.Getenv(key); v != "" {
			gitEnv = append(gitEnv, key+"="+v)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), harness.IdentityTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = gitEnv
	harness.KillGroup(cmd)
	return cmd.Output()
}

// dirtyContentHash binds a dirty tree's actual content: the HEAD-relative
// diff (tracked changes), the porcelain status, and the CONTENT of every
// untracked file (mid-arc review finding 8: the first version hashed
// only untracked NAMES, so two trees differing in an untracked file's
// body shared an identity). It hashes the tree gengo runs FROM — the
// same tree generatorRev names (cwd-git) — which for `go run` is the
// tree that built the binary; a prebuilt binary run elsewhere records
// that elsewhere-tree, consistent with its own cwd-git rev.
func dirtyContentHash(dir string) (string, error) {
	h := sha256.New()
	for _, args := range [][]string{
		{"-C", dir, "diff", "HEAD"},
		{"-C", dir, "status", "--porcelain"},
	} {
		out, err := gitProbe(args...)
		if err != nil {
			return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
		h.Write(out)
	}
	// Untracked file CONTENTS, in porcelain order. -uall descends into
	// untracked DIRECTORIES (E5: finding 8's fix stopped one level up —
	// files inside a new directory collapsed to a single "?? newdir/"
	// line, the same content-blind class) and -z stops git from quoting
	// unusual paths, which the line parser previously skipped silently.
	out, err := gitProbe("-C", dir, "status", "--porcelain", "-uall", "-z")
	if err != nil {
		return "", err
	}
	entries := strings.Split(string(out), "\x00")
	for i := 0; i < len(entries); i++ {
		entry := entries[i]
		if len(entry) < 4 {
			continue
		}
		xy, name := entry[:2], entry[3:]
		if xy[0] == 'R' || xy[0] == 'C' {
			// Rename/copy entries carry the original path as a second
			// NUL record; never an untracked entry, but it must not be
			// misread as one.
			i++
		}
		if xy != "??" {
			continue
		}
		path := filepath.Join(dir, name)
		if fi, err := os.Lstat(path); err != nil || !fi.Mode().IsRegular() {
			continue // specials: named by the status hash above
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("hashing untracked %s: %w", path, err)
		}
		h.Write([]byte(entry))
		h.Write([]byte{0})
		h.Write(b)
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// generatorRev is the generator's own identity in every artifact: VCS
// revision from build info, "-dirty" when the build had local changes.
// `go run` and test builds do not stamp VCS settings (audit M5 observed
// "unknown" in real artifacts), so the fallback asks git about the
// working directory — prefixed so the record is honest about its
// provenance.
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
	// Sanitized env inside gitProbe (hunt F4: an ambient GIT_DIR recorded
	// a FOREIGN repo's HEAD here).
	out, err := gitProbe("rev-parse", "HEAD")
	if err != nil {
		return "unknown"
	}
	rev = "cwd-git:" + strings.TrimSpace(string(out))
	if s, err := gitProbe("status", "--porcelain"); err != nil {
		rev += "-dirty-unknown" // a failed check must not read as clean
	} else if len(strings.TrimSpace(string(s))) > 0 {
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
