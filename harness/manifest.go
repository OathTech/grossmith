// Batch descriptor and pre-execution validation (evidence arc E3; audit
// P0: RunBatch discovered cases by glob and never verified file
// integrity, so an injected extra.go was built and judged while the
// recorded subject hash stayed clean, and a tampered subject was simply
// rehashed).
//
// The descriptor is the AUTHORITATIVE statement of what a batch is:
// exact case IDs and the sha256 of every build input, including the
// batch-root go.mod. ValidateBatch checks the tree against it — extra,
// missing, duplicate, digest-mismatched, or symlinked entries refuse the
// batch BEFORE anything executes. The remaining exposure is a concurrent
// mutator racing validation against the build (documented residual;
// closing it needs content-addressed snapshots — roadmap R4 territory).
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
	Schema       string            `json:"schema"`
	GeneratorRev string            `json:"generatorRev"`
	GoVersion    string            `json:"goVersion"`
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
//   - a file on disk beside the listed inputs (the injected-extra.go
//     replication);
//   - duplicate case IDs, or a case directory the descriptor never named;
//   - any symlink among the case files or root inputs.
func ValidateBatch(root string) ([]string, error) {
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
				return nil, fmt.Errorf("batch: %s/%s exists on disk but is not in the manifest — refusing the batch", mc.ID, e.Name())
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
	// Case directories the descriptor never named are foreign content.
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() && !seen[e.Name()] && caseDirRe.MatchString(e.Name()) {
			return nil, fmt.Errorf("batch: case directory %s on disk but not in the manifest", e.Name())
		}
	}
	sort.Strings(ids)
	return ids, nil
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
		return fmt.Errorf("batch: %s digest %s does not match the manifest's %s — the tree changed after generation", path, got[:12], want[:12])
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
