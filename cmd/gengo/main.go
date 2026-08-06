// gengo generates a batch of small, deterministic Go programs and optionally
// runs the conformance and cross-arch discrimination checks.
//
//	gengo -n 1000 -seed 1 -out out [-conformance] [-cross-arch 386]
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"grossmith/conform"
	"grossmith/gen"
)

func main() {
	n := flag.Int("n", 100, "number of programs to generate")
	seed := flag.Int64("seed", 1, "base seed; case i uses seed+i")
	out := flag.String("out", "out", "output directory")
	swarm := flag.Bool("swarm", true, "draw a per-seed construct mix")
	conformance := flag.Bool("conformance", false, "build+run every case against the pinned toolchain")
	crossArch := flag.String("cross-arch", "", "also run under this GOARCH (e.g. 386) and diff observations")
	timeout := flag.Duration("timeout", 10*time.Second, "per-case run timeout")
	workers := flag.Int("workers", runtime.NumCPU(), "parallel build/run workers")
	stats := flag.Bool("stats", false, "print the choice-frequency report (valid vs chosen per site)")
	flag.Parse()

	if err := run(*n, *seed, *out, *swarm, *conformance, *crossArch, *timeout, *workers, *stats); err != nil {
		fmt.Fprintln(os.Stderr, "gengo:", err)
		os.Exit(1)
	}
}

func run(n int, seed int64, out string, swarm, conformance bool, crossArch string, timeout time.Duration, workers int, stats bool) error {
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	// The batch root is its own throwaway module so `go test ./...` in the
	// repo never vets generated programs (vet's style checks — redundant
	// `v || v` and friends — legitimately fire on random code).
	modfile := filepath.Join(out, "go.mod")
	if _, err := os.Stat(modfile); os.IsNotExist(err) {
		if err := os.WriteFile(modfile, []byte("module grossmith-cases\n\ngo 1.26\n"), 0o644); err != nil {
			return err
		}
	}

	tagCount := map[string]int{}
	pairCount := map[string]int{}
	siteStats := map[string]*gen.SiteStats{}
	tagsByDir := map[string][]string{}
	var manifest strings.Builder
	manifest.WriteString("id\tseed\tfeatures\n")

	for i := 0; i < n; i++ {
		caseSeed := seed + int64(i)
		cfg := gen.DefaultConfig(caseSeed)
		cfg.Swarm = swarm
		c, err := gen.New(cfg).Generate()
		if err != nil {
			return fmt.Errorf("seed %d: %w", caseSeed, err)
		}
		id := fmt.Sprintf("case_%05d", i)
		dir := filepath.Join(out, id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "main.go"), c.Source, 0o644); err != nil {
			return err
		}
		// Features carry per-program COUNTS (tag=N): presence saturates for
		// common tags; counts keep them stratifiable.
		counted := make([]string, len(c.Features))
		for fi, f := range c.Features {
			counted[fi] = fmt.Sprintf("%s=%d", f, c.FeatureCounts[f])
		}
		fmt.Fprintf(&manifest, "%s\t%d\t%s\n", id, caseSeed, strings.Join(counted, ","))
		tagsByDir[dir] = c.Features
		for _, t := range c.Features {
			tagCount[t]++
		}
		for ai, a := range c.Features {
			for _, b := range c.Features[ai+1:] {
				pairCount[a+"+"+b]++
			}
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
	if err := os.WriteFile(filepath.Join(out, "manifest.tsv"), []byte(manifest.String()), 0o644); err != nil {
		return err
	}
	// Remove stale case dirs from a previous larger batch in the same out
	// dir: the conformance glob would otherwise mix two batches' verdicts
	// (review finding).
	stale, err := filepath.Glob(filepath.Join(out, "case_*"))
	if err != nil {
		return err
	}
	for _, dir := range stale {
		if _, live := tagsByDir[dir]; !live {
			if err := os.RemoveAll(dir); err != nil {
				return err
			}
		}
	}
	fmt.Printf("generated %d cases in %s (seeds %d..%d)\n", n, out, seed, seed+int64(n)-1)

	fmt.Println("\ncomposition (tag histogram):")
	printHistogram(tagCount, n)

	if stats {
		fmt.Println("\nchoice frequency (site/arm: valid -> chosen):")
		printSiteStats(siteStats)
		fmt.Println("\ntop tag co-occurrence pairs:")
		printTopPairs(pairCount, 15)
	}

	if !conformance && crossArch == "" {
		return nil
	}

	host := conform.Runtime{Name: "host"}
	rep, err := conform.Run(out, host, timeout, workers)
	if err != nil {
		return err
	}
	fmt.Printf("\nconformance statement:\n")
	fmt.Printf("  reference: %s GOARCH=%s\n", rep.GoVersion, runtime.GOARCH)
	fmt.Printf("  equivalence: byte equality of observations\n")
	fmt.Printf("  cases: %d  built: %d  ran: %d  timeouts: %d  panic-paths: %d  recovered: %d\n",
		rep.Total, rep.Built, rep.Ran, rep.TimedOut, rep.PanicPaths, rep.Recovered)
	fmt.Printf("  conformance rate: %.2f%%\n", 100*rep.Rate())
	for i, f := range rep.Failures {
		if i == 5 {
			fmt.Printf("  ... and %d more failures\n", len(rep.Failures)-5)
			break
		}
		fmt.Printf("  FAIL %s: %s\n", f.Dir, firstLine(f.Detail))
	}

	if crossArch != "" {
		alt := conform.Runtime{Name: crossArch, GOARCH: crossArch}
		altRep, err := conform.Run(out, alt, timeout, workers)
		if err != nil {
			return err
		}
		divs := conform.Diff(rep, altRep)
		fmt.Printf("\ncross-arch discrimination (host vs GOARCH=%s):\n", crossArch)
		fmt.Printf("  divergent cases: %d / %d\n", len(divs), rep.Total)
		inTag, offTag := 0, 0
		for _, d := range divs {
			if hasTag(tagsByDir[d.Dir], "width_dependent") {
				inTag++
			} else {
				offTag++
				fmt.Printf("  UNTAGGED divergence in %s (tag honesty violation)\n", d.Dir)
			}
		}
		widthTagged := tagCount["width_dependent"]
		fmt.Printf("  width_dependent-tagged cases: %d; divergences inside the tag: %d, outside: %d\n",
			widthTagged, inTag, offTag)
		if widthTagged > 0 {
			// The yield makes over-application visible: a tag on 90% of
			// cases with a 15% hit rate is honest but weak.
			fmt.Printf("  width_dependent yield: %d/%d (%.1f%%)\n",
				inTag, widthTagged, 100*float64(inTag)/float64(widthTagged))
		}
		switch {
		case len(divs) == 0:
			fmt.Println("  WARNING: no divergences — the observation may be blind, or all cases width-independent")
		case offTag == 0:
			fmt.Println("  discrimination proof: divergences exist and all fall inside the declared quotient")
		}
	}
	return nil
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

func printTopPairs(pairs map[string]int, top int) {
	keys := make([]string, 0, len(pairs))
	for k := range pairs {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if pairs[keys[i]] != pairs[keys[j]] {
			return pairs[keys[i]] > pairs[keys[j]]
		}
		return keys[i] < keys[j]
	})
	for i, k := range keys {
		if i == top {
			break
		}
		fmt.Printf("  %-40s %d\n", k, pairs[k])
	}
}
