// Package conform builds and runs generated cases against the pinned Go
// toolchain and reports the conformance rate — tracked, never gated. It also
// runs the cross-arch discrimination check: the cheapest possible
// "deliberately divergent clone" is the same toolchain at a different GOARCH.
package conform

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// CaseResult is the verdict on one case under one runtime.
type CaseResult struct {
	Dir       string
	Built     bool
	Ran       bool // exit status 0 within the timeout
	TimedOut  bool
	PanicPath bool // the observation is a recovered panic
	Output    string
	Detail    string // compiler or runtime error text when something failed
}

// Conformant reports the charter property: accepted and run to completion.
func (r CaseResult) Conformant() bool { return r.Built && r.Ran }

// Runtime names one way to build a case. GOARCH empty means the host arch.
type Runtime struct {
	Name   string
	GOARCH string
}

// Check builds and runs one case directory (containing main.go) under rt.
func Check(dir string, rt Runtime, timeout time.Duration) CaseResult {
	res := CaseResult{Dir: dir}
	bin := "case.bin"
	if rt.GOARCH != "" {
		bin = "case-" + rt.GOARCH + ".bin"
	}
	build := exec.Command("go", "build", "-o", bin, "main.go")
	build.Dir = dir
	if rt.GOARCH != "" {
		build.Env = append(os.Environ(), "GOARCH="+rt.GOARCH, "CGO_ENABLED=0")
	}
	if out, err := build.CombinedOutput(); err != nil {
		res.Detail = fmt.Sprintf("build: %v: %s", err, out)
		return res
	}
	res.Built = true

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	abs, err := filepath.Abs(filepath.Join(dir, bin))
	if err != nil {
		res.Detail = fmt.Sprintf("abs: %v", err)
		return res
	}
	run := exec.CommandContext(ctx, abs)
	out, err := run.CombinedOutput()
	res.Output = string(out)
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		res.TimedOut = true
		res.Detail = "timeout"
	case err != nil:
		res.Detail = fmt.Sprintf("run: %v: %s", err, out)
	default:
		res.Ran = true
		res.PanicPath = strings.HasPrefix(res.Output, "panic")
	}
	return res
}

// Report aggregates one runtime's results over a batch.
type Report struct {
	Runtime    Runtime
	GoVersion  string
	Total      int
	Built      int
	Ran        int
	TimedOut   int
	PanicPaths int
	Failures   []CaseResult
	Results    []CaseResult // per case, in directory order
}

// Rate is the conformance rate in [0,1].
func (r Report) Rate() float64 {
	if r.Total == 0 {
		return 0
	}
	return float64(r.Ran) / float64(r.Total)
}

// CaseDirs lists the case directories under root, sorted.
func CaseDirs(root string) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(root, "*", "main.go"))
	if err != nil {
		return nil, err
	}
	dirs := make([]string, 0, len(matches))
	for _, m := range matches {
		dirs = append(dirs, filepath.Dir(m))
	}
	sort.Strings(dirs)
	return dirs, nil
}

// Run checks every case under root against rt with the given parallelism.
func Run(root string, rt Runtime, timeout time.Duration, workers int) (Report, error) {
	dirs, err := CaseDirs(root)
	if err != nil {
		return Report{}, err
	}
	if len(dirs) == 0 {
		return Report{}, fmt.Errorf("no cases under %s", root)
	}
	if workers < 1 {
		workers = 1
	}
	rep := Report{Runtime: rt, GoVersion: GoVersion(), Total: len(dirs)}
	rep.Results = make([]CaseResult, len(dirs))

	var wg sync.WaitGroup
	jobs := make(chan int)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				rep.Results[i] = Check(dirs[i], rt, timeout)
			}
		}()
	}
	for i := range dirs {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	for _, res := range rep.Results {
		if res.Built {
			rep.Built++
		}
		if res.Ran {
			rep.Ran++
		}
		if res.TimedOut {
			rep.TimedOut++
		}
		if res.PanicPath {
			rep.PanicPaths++
		}
		if !res.Conformant() {
			rep.Failures = append(rep.Failures, res)
		}
	}
	return rep, nil
}

// Divergence is one case whose observations differ between two runtimes.
type Divergence struct {
	Dir      string
	OutputA  string
	OutputB  string
	DetailA  string
	DetailB  string
}

// Diff compares two per-case result sets (same directory order) by byte
// equality of observations — the conformance equivalence. Cases that failed
// to build or run under either runtime are reported as divergences with
// their details, fail-closed.
func Diff(a, b Report) []Divergence {
	var out []Divergence
	for i := range a.Results {
		ra, rb := a.Results[i], b.Results[i]
		if ra.Conformant() && rb.Conformant() && bytes.Equal([]byte(ra.Output), []byte(rb.Output)) {
			continue
		}
		out = append(out, Divergence{
			Dir: ra.Dir, OutputA: ra.Output, OutputB: rb.Output,
			DetailA: ra.Detail, DetailB: rb.Detail,
		})
	}
	return out
}

// GoVersion reports the pinned toolchain doing the building.
func GoVersion() string {
	out, err := exec.Command("go", "version").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}
