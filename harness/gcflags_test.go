package harness

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestGcCompilerFlags(t *testing.T) {
	ctx := context.Background()
	ref := &GcAdapter{}
	clone := &GcAdapter{GCFlags: "-N -l"}
	refID, err := ref.Identity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	cloneID, err := clone.Identity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if refID == cloneID || !strings.Contains(cloneID, `gcflags="-N -l"`) {
		t.Fatalf("identities omit compiler configuration: reference=%q clone=%q", refID, cloneID)
	}
	oracle, err := clone.Oracle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if oracle.GCFlags != clone.GCFlags {
		t.Fatalf("structured identity flags = %q", oracle.GCFlags)
	}
	if testing.Short() {
		return
	}
	root := t.TempDir()
	writeCases(t, root, 1, 42)
	// A rejected compiler option proves flags reach the actual compiler.
	// Merely recording them in an identity could otherwise pass unnoticed.
	clone.GCFlags = "-grossmith-unknown-compiler-option"
	outcome := clone.Run(ctx, filepath.Join(root, "case_00000"))
	if outcome.Status != StatusBuildFailed || !strings.Contains(outcome.Detail, "grossmith-unknown-compiler-option") {
		t.Fatalf("compiler flags were not applied: %+v", outcome)
	}
}
