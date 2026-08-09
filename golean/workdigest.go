package golean

// Clone work-tree digests (evidence arc E5, arc-end review B2). The
// reference side's build inputs are digested by the batch descriptor,
// but the CLONE compiles from golean-work/cases/<id>/main.go — a tree
// the descriptor never covered, so "which bytes both adapters executed"
// was only half answerable offline. translate() writes main.go as a
// byte copy of subject.go; these digests turn that from an assumption
// into a recorded, re-checkable fact.
//
// SCOPE (E5 re-review): the digests cover the translated case sources
// under cases/ and the three named run artifacts at the work root —
// nothing else. In particular, diff-coverage makes its OWN copy of each
// main.go under artifacts/go-run/<id>/ (alongside its harness file) and
// hands THAT to `go run`; artifacts/ and diff-coverage.log are
// diagnostics outside every digest. The bytes executed on that nested
// path are provable only TRANSITIVELY: the recorded script sha256 plus
// the checkout identity pin the copier, and the copier's input is the
// digested cases/<id>/main.go. Direct coverage of the nested copies
// would digest a tree their script owns the layout of — deferred, and
// the batch's verification summary claims only what is direct.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"grossmith/harness"
)

// workFileNames is the closed set of run artifacts at the work root:
// the translated manifest we wrote and the results the script published.
var workFileNames = []string{"manifest.tsv", "results.tsv", "results.tsv.meta"}

var workDigestRe = regexp.MustCompile("^[0-9a-f]{64}$")

// WorkDigests digests a campaign's work tree after the run: per-case
// main.go digests (keyed by case ID) and the work-root run artifacts.
// Unexpected entries refuse — a file this function does not understand
// is a file no digest vouches for.
func WorkDigests(workDir string) (perCase, workFiles map[string]string, err error) {
	perCase, workFiles = map[string]string{}, map[string]string{}
	caseRoot := filepath.Join(workDir, "cases")
	entries, err := os.ReadDir(caseRoot)
	if os.IsNotExist(err) {
		return perCase, workFiles, nil // no case was translated
	}
	if err != nil {
		return nil, nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			return nil, nil, fmt.Errorf("golean work: unexpected file %s at the case root", e.Name())
		}
		dir := filepath.Join(caseRoot, e.Name())
		files, err := os.ReadDir(dir)
		if err != nil {
			return nil, nil, err
		}
		for _, f := range files {
			if f.Name() != "main.go" {
				return nil, nil, fmt.Errorf("golean work: unexpected entry %s in case %s — the translated case is exactly main.go", f.Name(), e.Name())
			}
		}
		sum, err := harness.FileSHA256(filepath.Join(dir, "main.go"))
		if err != nil {
			return nil, nil, err
		}
		perCase[e.Name()] = sum
	}
	for _, name := range workFileNames {
		sum, err := harness.FileSHA256(filepath.Join(workDir, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		workFiles[name] = sum
	}
	return perCase, workFiles, nil
}

// VerifyWork re-checks a published batch's clone work tree against the
// report's recorded digests: every recorded case source must be on disk
// with its digest, equal to the subject digest (the byte-copy claim),
// and nothing may exist that no digest names.
func VerifyWork(workDir string, rep harness.BatchReport) error {
	recordedCases := map[string]string{}
	for _, cr := range rep.Cases {
		if cr.CloneSourceSHA256 == "" {
			continue
		}
		if !workDigestRe.MatchString(cr.CloneSourceSHA256) {
			return fmt.Errorf("golean verify: case %s cloneSourceSha256 %q is not a sha256 digest", cr.ID, cr.CloneSourceSHA256)
		}
		if cr.CloneSourceSHA256 != cr.SubjectSHA256 {
			return fmt.Errorf("golean verify: case %s clone source digest %s differs from the subject's %s — the clone compiled different bytes than the batch describes",
				cr.ID, harness.ShortDigest(cr.CloneSourceSHA256), harness.ShortDigest(cr.SubjectSHA256))
		}
		recordedCases[cr.ID] = cr.CloneSourceSHA256
	}
	onDisk, diskFiles, err := WorkDigests(workDir)
	if err != nil {
		return err
	}
	for id, want := range recordedCases {
		got, ok := onDisk[id]
		if !ok {
			return fmt.Errorf("golean verify: case %s is recorded but missing from the work tree", id)
		}
		if got != want {
			return fmt.Errorf("golean verify: case %s main.go digest %s does not match the recorded %s — the clone's source changed after the batch finished",
				id, harness.ShortDigest(got), harness.ShortDigest(want))
		}
	}
	for id := range onDisk {
		if _, ok := recordedCases[id]; !ok {
			return fmt.Errorf("golean verify: work tree contains case %s that the report does not record", id)
		}
	}
	for name, want := range rep.CloneWorkFiles {
		got, ok := diskFiles[name]
		if !ok {
			return fmt.Errorf("golean verify: recorded work file %s is missing", name)
		}
		if got != want {
			return fmt.Errorf("golean verify: %s digest %s does not match the recorded %s", name, harness.ShortDigest(got), harness.ShortDigest(want))
		}
	}
	for name := range diskFiles {
		if _, ok := rep.CloneWorkFiles[name]; !ok {
			return fmt.Errorf("golean verify: work file %s exists but the report does not record it", name)
		}
	}
	return nil
}
