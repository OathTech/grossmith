// Batch descriptor and pre-execution validation (evidence arc E3).
//
// A batch is meant to be a REPRODUCIBLE EXPERIMENT: batch.json names
// the exact programs a verdict was computed over. The audit found that
// claim unenforced — RunBatch discovered cases by glob and verified
// nothing, so whatever sat in a case directory got compiled while the
// recorded hash described only subject.go. Trees drift for ordinary
// reasons: a leftover file from a previous run, a half-written case
// from an interrupted one, an editor backup, a hand-edit during
// debugging. Any of them silently changes what was measured.
//
// The descriptor is therefore the AUTHORITATIVE statement of what a
// batch is: exact case IDs and the sha256 of every build input,
// including the batch-root go.mod. ValidateBatch checks the tree
// against it — extra, missing, duplicate, digest-mismatched, or
// symlinked entries refuse the batch BEFORE anything executes, so a
// drifted tree is a loud refusal instead of a quiet misattribution.
// Residual: validation and the build are separate steps, so a tree
// edited BETWEEN them is not covered; closing that needs
// content-addressed snapshots (roadmap R4 territory).
package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
)

const ManifestSchema = "grossmith-manifest-v1"

// caseDirRe is the batch-owned case-directory shape. Every case ID in a
// descriptor must match it (2026-08-10 audit, P0, replicated: the regex
// was declared and never applied, so `ValidateBatch` accepted a case ID
// of `../outside` — the root closure was satisfied because its `seen`
// key was literally that string — and returned it for judging, which
// then compiled a directory outside the batch. E3's defining claim is
// that the descriptor names a CLOSED tree, so the shape check belongs
// before any filesystem access).
var caseDirRe = regexp.MustCompile("^case_[0-9]+$")

// caseFileRe is the closed case-input file schema. Every file the
// generator writes into a case directory (and nothing else) matches it:
// subject.go, driver.go, case.json.
var caseFileRe = regexp.MustCompile(`^(subject\.go|driver\.go|case\.json)$`)

// rootFileNames is the closed set of digested batch-root inputs. Only
// go.mod affects a build; the other root artifacts are bound by
// complete.json's reportFiles instead.
var rootFileNames = map[string]bool{"go.mod": true}

// localName reports whether name is a single path component that names
// something INSIDE the directory it is joined to: no separators, no
// traversal, no absolute path, no cleaning change (so "a/", "./a" and
// "a/../b" are all refused rather than silently normalized).
func localName(name string) bool {
	return name != "" && name != "." && name != ".." &&
		filepath.IsLocal(name) && filepath.Base(name) == name &&
		filepath.Clean(name) == name
}

// digestRe is the only digest shape the batch machinery accepts. Checked
// on every digest READ from disk, so comparison and truncation never see
// a malformed value (arc-end review B3: an unguarded [:12] on a
// descriptor field panicked the verifier — a corruption detector must
// refuse corruption, not crash on it).
var digestRe = regexp.MustCompile("^[0-9a-f]{64}$")

// ShortDigest abbreviates a digest for error messages, tolerating any
// input length.
func ShortDigest(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

// ManifestCase is one case's build-input digests. Files maps file name
// (within the case directory) to sha256 — the COMPLETE set: a file on
// disk that is not listed here fails validation.
type ManifestCase struct {
	ID    string            `json:"id"`
	Seed  int64             `json:"seed"`
	Files map[string]string `json:"files"`
}

// Manifest is the batch descriptor, written after the last case and
// before any judging.
type Manifest struct {
	Schema       string `json:"schema"`
	GeneratorRev string `json:"generatorRev"`
	GoVersion    string `json:"goVersion"`
	// RootFiles digests batch-root build inputs (go.mod).
	RootFiles map[string]string `json:"rootFiles"`
	Cases     []ManifestCase    `json:"cases"`
}

// WriteManifest digests the named case directories and writes the
// descriptor. Every regular file in each case directory is included —
// the descriptor claims the whole directory, so nothing can hide beside
// the listed inputs.
func WriteManifest(root, generatorRev, goVersion string, ids []string, seeds map[string]int64) (Manifest, error) {
	m := Manifest{Schema: ManifestSchema, GeneratorRev: generatorRev, GoVersion: goVersion,
		RootFiles: map[string]string{}}
	sum, err := FileSHA256(filepath.Join(root, "go.mod"))
	if err != nil {
		return Manifest{}, fmt.Errorf("manifest: %w", err)
	}
	m.RootFiles["go.mod"] = sum
	for _, id := range ids {
		// The producer is held to the same closed shapes the reader
		// enforces, so a descriptor can never be written that
		// ReadManifest would refuse (2026-08-10 audit P0).
		if !localName(id) || !caseDirRe.MatchString(id) {
			return Manifest{}, fmt.Errorf("manifest: case ID %q is not a batch-owned case directory (want %s)", id, caseDirRe)
		}
		mc := ManifestCase{ID: id, Seed: seeds[id], Files: map[string]string{}}
		dir := filepath.Join(root, id)
		entries, err := os.ReadDir(dir)
		if err != nil {
			return Manifest{}, fmt.Errorf("manifest: %w", err)
		}
		for _, e := range entries {
			if err := regularFile(dir, e); err != nil {
				return Manifest{}, err
			}
			if !localName(e.Name()) || !caseFileRe.MatchString(e.Name()) {
				return Manifest{}, fmt.Errorf("manifest: case %s holds %q, outside the closed case-input schema (subject.go, driver.go, case.json)", id, e.Name())
			}
			sum, err := FileSHA256(filepath.Join(dir, e.Name()))
			if err != nil {
				return Manifest{}, err
			}
			mc.Files[e.Name()] = sum
		}
		m.Cases = append(m.Cases, mc)
	}
	b, err := json.MarshalIndent(m, "", " ")
	if err != nil {
		return Manifest{}, err
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), b, 0o644); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

// ReadManifest strict-decodes the descriptor.
func ReadManifest(root string) (Manifest, error) {
	b, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("manifest: %w", err)
	}
	if m.Schema != ManifestSchema {
		return Manifest{}, fmt.Errorf("manifest: unknown schema %q", m.Schema)
	}
	// CONTAINMENT FIRST (2026-08-10 audit P0): every name a descriptor
	// supplies is checked for shape BEFORE it is joined to a path or
	// handed to an adapter, so a descriptor can only ever describe files
	// inside the batch tree. Digest shape is checked in the same pass, so
	// nothing downstream compares or abbreviates a malformed value.
	for name, d := range m.RootFiles {
		if !localName(name) || !rootFileNames[name] {
			return Manifest{}, fmt.Errorf("manifest: rootFiles names %q — the closed batch-root input set is {go.mod}", name)
		}
		if !digestRe.MatchString(d) {
			return Manifest{}, fmt.Errorf("manifest: rootFiles[%s] %q is not a sha256 digest", name, d)
		}
	}
	seen := map[string]bool{}
	for _, mc := range m.Cases {
		if !localName(mc.ID) || !caseDirRe.MatchString(mc.ID) {
			return Manifest{}, fmt.Errorf("manifest: case ID %q is not a batch-owned case directory (want %s) — a descriptor may only name cases INSIDE its batch", mc.ID, caseDirRe)
		}
		if seen[mc.ID] {
			return Manifest{}, fmt.Errorf("manifest: duplicate case ID %s", mc.ID)
		}
		seen[mc.ID] = true
		if len(mc.Files) == 0 {
			return Manifest{}, fmt.Errorf("manifest: case %s lists no files", mc.ID)
		}
		for name, d := range mc.Files {
			if !localName(name) || !caseFileRe.MatchString(name) {
				return Manifest{}, fmt.Errorf("manifest: case %s names file %q — the closed case-input schema is subject.go, driver.go, case.json", mc.ID, name)
			}
			if !digestRe.MatchString(d) {
				return Manifest{}, fmt.Errorf("manifest: %s/%s digest %q is not a sha256 digest", mc.ID, name, d)
			}
		}
	}
	return m, nil
}

// ValidateBatch checks the tree against its descriptor and returns the
// ordered case IDs. Refusals, all pre-execution:
//   - a name that is not a batch-owned case ID or a closed-schema input
//     file (ReadManifest, before any path is joined — a descriptor
//     cannot reach outside its own tree);
//   - a listed case directory or file missing, or its digest changed;
//   - a file on disk beside the listed inputs (the extra-file case the
//     audit reproduced: an unlisted .go file compiles into the case);
//   - duplicate case IDs, or a case directory the descriptor never named;
//   - any symlink among the case files or root inputs.
func ValidateBatch(root string) ([]string, error) {
	// The root itself and every case directory must be REAL directories
	// (mid-arc review finding 3: a symlinked case dir passed — ReadDir
	// follows it while Lstat only ever saw the final components, so the
	// bytes actually compiled could live anywhere and change without
	// the batch tree showing it).
	if err := realDir(root); err != nil {
		return nil, err
	}
	m, err := ReadManifest(root)
	if err != nil {
		return nil, err
	}
	for name, want := range m.RootFiles {
		if err := checkFile(root, name, want); err != nil {
			return nil, err
		}
	}
	// IDs and file names are shape-checked by ReadManifest, so every join
	// below stays inside root; duplicates are refused there too.
	seen := map[string]bool{}
	var ids []string
	for _, mc := range m.Cases {
		seen[mc.ID] = true
		dir := filepath.Join(root, mc.ID)
		if err := realDir(dir); err != nil {
			return nil, err
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, fmt.Errorf("batch: declared case missing: %w", err)
		}
		onDisk := map[string]bool{}
		for _, e := range entries {
			if err := regularFile(dir, e); err != nil {
				return nil, err
			}
			onDisk[e.Name()] = true
			if _, listed := mc.Files[e.Name()]; !listed {
				return nil, fmt.Errorf("batch: %s/%s exists on disk but is not in the manifest — it would be compiled into the case, so the batch is refused", mc.ID, e.Name())
			}
		}
		for name, want := range mc.Files {
			if !onDisk[name] {
				return nil, fmt.Errorf("batch: %s/%s is in the manifest but missing on disk", mc.ID, name)
			}
			if err := checkFile(dir, name, want); err != nil {
				return nil, err
			}
		}
		ids = append(ids, mc.ID)
	}
	// The ROOT is closed too (mid-arc review finding 4: "every build
	// input digested" was overbroad — an unlisted root extra.go,
	// go.work, or vendor/ was accepted; none changes a judged build
	// under the pinned env today, but the descriptor's claim is the
	// whole tree, so it should mean the whole tree).
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		name := e.Name()
		switch {
		case e.Type()&os.ModeSymlink != 0:
			// Before the allowlist: a symlink under an allowlisted NAME
			// (manifest.json included) would otherwise pass, pointing the
			// descriptor itself somewhere the batch tree cannot vouch for
			// (E5; the mid-arc symlink findings' recorded root residual).
			return nil, fmt.Errorf("batch: %s at the batch root is a symlink — refused", name)
		case e.IsDir() && seen[name]:
		case e.IsDir() && name == "golean-work": // judge-time artifact tree
		case !e.IsDir() && rootFileAllowed(name):
		default:
			return nil, fmt.Errorf("batch: %s at the batch root is neither a declared case nor a batch artifact — refusing", name)
		}
	}
	// complete.json, when present, must bind to THIS manifest (mid-arc
	// review finding 6: it was written and never read). Presence is
	// required by VerifyBatch — a pre-judge staging tree does not have
	// it yet, so ValidateBatch alone tolerates absence.
	if _, err := checkComplete(root); err != nil && err != errNoComplete {
		return nil, err
	}
	sort.Strings(ids)
	return ids, nil
}

// VerifyInfo is VerifyBatch's result: the ordered case IDs plus what the
// completion descriptor actually covered, so the caller's summary can
// state its scope instead of overclaiming (arc-end review B1: an edited
// batch.json passed -verify with "every input digest intact").
type VerifyInfo struct {
	IDs []string
	// ReportsBound: complete.json carried report digests and they all
	// matched. False when reportFiles is absent — a batch written before
	// the binding existed, or a descriptor a later hand-edit removed the
	// field from; the two are indistinguishable offline, so the caller
	// must treat the reduced scope as a refusal unless explicitly told
	// otherwise, and say what went unchecked either way.
	ReportsBound bool
}

// VerifyBatch is ValidateBatch for a PUBLISHED batch of record: the
// completion descriptor is required, not optional (finding 7's consumer:
// `gengo -verify`).
func VerifyBatch(root string) (VerifyInfo, error) {
	c, err := checkComplete(root)
	if err == errNoComplete {
		return VerifyInfo{}, fmt.Errorf("batch: no completion descriptor — an interrupted or pre-E3 run, not a batch of record")
	}
	if err != nil {
		return VerifyInfo{}, err
	}
	ids, err := ValidateBatch(root)
	if err != nil {
		return VerifyInfo{}, err
	}
	return VerifyInfo{IDs: ids, ReportsBound: c.ReportFiles != nil}, nil
}

const CompleteSchema = "grossmith-complete-v1"

// Complete is the completion descriptor, written LAST into a batch: a
// tree without it is an interrupted run, never a batch of record. It
// binds the manifest, and (since E5) the report artifacts, by digest —
// batch.json is where verdicts live, so leaving it unbound let an edited
// total, verdict, or identity pass -verify untouched.
type Complete struct {
	Schema         string `json:"schema"`
	ManifestSHA256 string `json:"manifestSha256"`
	// ReportFiles digests the batch's report artifacts (batch.json when
	// the batch was judged, manifest.tsv always). Every batch this code
	// writes carries it; absence means an older producer or a later
	// hand-edit, which read the same offline — VerifyBatch reports the
	// distinction it cannot make and the CLI refuses by default. When
	// present it is CLOSED — a report artifact on disk that it does not
	// name refuses.
	ReportFiles map[string]string `json:"reportFiles,omitempty"`
}

// reportArtifacts is the closed set of files ReportFiles may cover.
var reportArtifacts = []string{"batch.json", "manifest.tsv"}

// WriteComplete digests the manifest and the report artifacts present in
// root and writes the completion descriptor. Call it after every other
// batch file is final.
func WriteComplete(root string) error {
	sum, err := FileSHA256(filepath.Join(root, "manifest.json"))
	if err != nil {
		return err
	}
	c := Complete{Schema: CompleteSchema, ManifestSHA256: sum}
	for _, name := range reportArtifacts {
		rs, err := FileSHA256(filepath.Join(root, name))
		if os.IsNotExist(err) {
			continue // an unjudged batch has no batch.json
		}
		if err != nil {
			return err
		}
		if c.ReportFiles == nil {
			c.ReportFiles = map[string]string{}
		}
		c.ReportFiles[name] = rs
	}
	b, err := json.MarshalIndent(c, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, "complete.json"), b, 0o644)
}

// errNoComplete: the tree has no completion descriptor. ValidateBatch
// tolerates it (a staging tree is validated before judging); VerifyBatch
// refuses.
var errNoComplete = fmt.Errorf("no complete.json")

// checkComplete reads and checks the completion descriptor, FAIL-CLOSED
// (arc-end review B4: the old check sat inside `if err == nil`, so an
// unreadable complete.json skipped the binding and still printed
// success; schema was decoded but never compared; unknown fields were
// accepted, so a misspelled field silently unbound).
func checkComplete(root string) (Complete, error) {
	path := filepath.Join(root, "complete.json")
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Complete{}, errNoComplete
	}
	if err != nil {
		// Present but unreadable is a refusal, never a skip.
		return Complete{}, fmt.Errorf("batch: completion descriptor unreadable: %w", err)
	}
	var c Complete
	dec := json.NewDecoder(bytes.NewReader(b))
	// Strict on purpose: this is an integrity descriptor, so a field the
	// checker does not understand must refuse rather than silently not
	// bind. Additive evolution goes through a schema bump.
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return Complete{}, fmt.Errorf("batch: complete.json: %w", err)
	}
	if c.Schema != CompleteSchema {
		return Complete{}, fmt.Errorf("batch: complete.json schema %q, want %q", c.Schema, CompleteSchema)
	}
	if !digestRe.MatchString(c.ManifestSHA256) {
		// Guards the truncation below too (B3: a `{}` descriptor panicked
		// the checker on an empty-string slice).
		return Complete{}, fmt.Errorf("batch: complete.json manifestSha256 %q is not a sha256 digest", c.ManifestSHA256)
	}
	got, err := FileSHA256(filepath.Join(root, "manifest.json"))
	if err != nil {
		return Complete{}, err
	}
	if c.ManifestSHA256 != got {
		return Complete{}, fmt.Errorf("batch: complete.json binds manifest %s but the manifest on disk is %s", ShortDigest(c.ManifestSHA256), ShortDigest(got))
	}
	if c.ReportFiles != nil {
		listed := map[string]bool{}
		for name, want := range c.ReportFiles {
			if !allowedReportArtifact(name) {
				return Complete{}, fmt.Errorf("batch: complete.json reportFiles names %q, which is not a report artifact", name)
			}
			if !digestRe.MatchString(want) {
				return Complete{}, fmt.Errorf("batch: complete.json reportFiles[%s] %q is not a sha256 digest", name, want)
			}
			listed[name] = true
			got, err := FileSHA256(filepath.Join(root, name))
			if err != nil {
				return Complete{}, fmt.Errorf("batch: reportFiles lists %s but it cannot be read: %w", name, err)
			}
			if got != want {
				return Complete{}, fmt.Errorf("batch: %s digest %s does not match complete.json's %s — the report changed after the batch finished", name, ShortDigest(got), ShortDigest(want))
			}
		}
		// Closed the other way too: a report artifact on disk that the
		// descriptor does not name is a report nobody vouched for.
		for _, name := range reportArtifacts {
			if listed[name] {
				continue
			}
			if _, err := os.Lstat(filepath.Join(root, name)); err == nil {
				return Complete{}, fmt.Errorf("batch: %s exists but complete.json's reportFiles does not name it", name)
			}
		}
	}
	return c, nil
}

func allowedReportArtifact(name string) bool {
	for _, a := range reportArtifacts {
		if name == a {
			return true
		}
	}
	return false
}

// rootFileAllowed is the closed set of batch-root artifacts.
func rootFileAllowed(name string) bool {
	switch name {
	case "go.mod", "manifest.tsv", "manifest.json", "batch.json", "complete.json", gengoStagingMarker:
		return true
	}
	return false
}

// GengoStagingMarker names the ownership marker gengo writes FIRST into
// a staging tree, so crash-recovery can tell its own leftovers from
// foreign directories (mid-arc review finding 2).
const gengoStagingMarker = ".gengo-staging"

// StagingMarker exposes the marker name to the producer.
func StagingMarker() string { return gengoStagingMarker }

// realDir refuses symlinked or non-directory path components at the
// levels the batch owns.
func realDir(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("batch: %w", err)
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("batch: %s is a symlink — refused", path)
	}
	if !fi.IsDir() {
		return fmt.Errorf("batch: %s is not a directory", path)
	}
	return nil
}

func checkFile(dir, name, want string) error {
	path := filepath.Join(dir, name)
	fi, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("batch: %w", err)
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("batch: %s is not a regular file (mode %s) — symlinks and specials are refused", path, fi.Mode())
	}
	got, err := FileSHA256(path)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("batch: %s digest %s does not match the manifest's %s — this file changed after the batch was generated", path, ShortDigest(got), ShortDigest(want))
	}
	return nil
}

func regularFile(dir string, e os.DirEntry) error {
	if e.IsDir() {
		return fmt.Errorf("batch: unexpected directory %s inside a case", filepath.Join(dir, e.Name()))
	}
	fi, err := os.Lstat(filepath.Join(dir, e.Name()))
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("batch: %s is not a regular file (mode %s)", filepath.Join(dir, e.Name()), fi.Mode())
	}
	return nil
}
