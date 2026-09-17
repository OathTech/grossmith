package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"grossmith/gen"
	"grossmith/harness"
)

// loadCheck snapshots existing inputs before staging a new run. Directory
// input preserves both files exactly; file input gets a current driver whose
// arity follows the edited subject. Neither trusts potentially stale case.json
// metadata or claims that edited source came from the generator's draw tape.
func loadCheck(cfg config) (*gen.Case, *harness.CaseOrigin, error) {
	for name := range cfg.explicit {
		switch name {
		case "check", "out", "judge", "clone", "go", "clone-go", "clone-gcflags",
			"panic-policy", "timeout", "workers", "allow-dirty":
		default:
			return nil, nil, fmt.Errorf("-%s does not apply to -check", name)
		}
	}
	if !cfg.explicit["out"] || cfg.out == "" {
		return nil, nil, fmt.Errorf("-check requires an explicit, separate -out directory")
	}
	// A check is a new experiment. Keep both the input and previous runs;
	// the user picks a fresh destination for each comparison.
	entries, err := os.ReadDir(cfg.out)
	if err != nil && !os.IsNotExist(err) {
		return nil, nil, err
	}
	if len(entries) > 0 {
		return nil, nil, fmt.Errorf("-check requires an empty or absent -out directory: %s", cfg.out)
	}
	input, err := filepath.Abs(cfg.check)
	if err != nil {
		return nil, nil, err
	}
	info, err := os.Stat(input)
	if err != nil {
		return nil, nil, err
	}
	sourcePath := input
	driverPath := ""
	origin := &harness.CaseOrigin{Kind: "source-check", Path: input, Driver: "current"}
	if info.IsDir() {
		sourcePath = filepath.Join(input, "subject.go")
		driverPath = filepath.Join(input, "driver.go")
		origin.Driver = "copied"
	}
	// Publication can clean up an interrupted staging/previous directory.
	// Reject inputs inside any of those paths, including symlink aliases.
	for _, file := range []string{sourcePath, driverPath} {
		if file == "" {
			continue
		}
		resolved, err := resolvedPath(file)
		if err != nil {
			return nil, nil, err
		}
		for _, tree := range []string{cfg.out, cfg.out + ".staging", cfg.out + ".prev"} {
			root, err := resolvedPath(tree)
			if err != nil {
				return nil, nil, err
			}
			rel, err := filepath.Rel(root, resolved)
			if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return nil, nil, fmt.Errorf("check input %s is inside output tree %s", file, tree)
			}
		}
	}
	source, err := os.ReadFile(sourcePath)
	if err != nil {
		return nil, nil, err
	}
	var driver []byte
	if driverPath != "" {
		driver, err = os.ReadFile(driverPath)
	} else {
		driver, err = gen.DriverForSource(source)
	}
	if err != nil {
		return nil, nil, err
	}
	return &gen.Case{Source: source, Driver: driver, FeatureCounts: map[string]int{}}, origin, nil
}

// resolvedPath resolves existing ancestors too, so a new output below a
// symlinked parent is compared with input paths in the same coordinate space.
func resolvedPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return resolved, nil
	}
	if !os.IsNotExist(err) || filepath.Dir(abs) == abs {
		return "", err
	}
	parent, err := resolvedPath(filepath.Dir(abs))
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(abs)), nil
}
