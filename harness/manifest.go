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

// caseDirRe is the batch-owned case-directory shape.
var caseDirRe = regexp.MustCompile("^case_[0-9]+$")

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
	return m, nil
}

// ValidateBatch checks the tree against its descriptor and returns the
// ordered case IDs. Refusals, all pre-execution:
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
	seen := map[string]bool{}
	var ids []string
	for _, mc := range m.Cases {
		if seen[mc.ID] {
			return nil, fmt.Errorf("batch: duplicate case ID %s in manifest", mc.ID)
		}
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
	if sum, err := FileSHA256(filepath.Join(root, "complete.json")); err == nil {
		_ = sum
		var c struct {
			Schema         string `json:"schema"`
			ManifestSHA256 string `json:"manifestSha256"`
		}
		b, err := os.ReadFile(filepath.Join(root, "complete.json"))
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(b, &c); err != nil {
			return nil, fmt.Errorf("complete.json: %w", err)
		}
		got, err := FileSHA256(filepath.Join(root, "manifest.json"))
		if err != nil {
			return nil, err
		}
		if c.ManifestSHA256 != got {
			return nil, fmt.Errorf("batch: complete.json binds manifest %s but the manifest on disk is %s", c.ManifestSHA256[:12], got[:12])
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// VerifyBatch is ValidateBatch for a PUBLISHED batch of record: the
// completion descriptor is required, not optional (finding 7's consumer:
// `gengo -verify`).
func VerifyBatch(root string) ([]string, error) {
	if _, err := os.Stat(filepath.Join(root, "complete.json")); err != nil {
		return nil, fmt.Errorf("batch: no completion descriptor — an interrupted or pre-E3 run, not a batch of record: %w", err)
	}
	return ValidateBatch(root)
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
		return fmt.Errorf("batch: %s digest %s does not match the manifest's %s — this file changed after the batch was generated", path, got[:12], want[:12])
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
