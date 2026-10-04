package golean

// Kept in step with cmd/gengo/goleancheckout_test.go (test helpers cannot be
// shared across packages without a non-test package).

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// goleanMinRevision is the oldest GoLean commit these integration tests
// accept: it adds byte-string panic support, which the panic-message
// witnesses compare against. A deps/golean checkout predating it failed
// TestCheckGoLeanPanicMessages (cmd/gengo) under a plain `go test ./...`.
const goleanMinRevision = "3bb8f4fc9cd7dab16571787140731b6d4c1f9d0e"

// goleanCheckoutAtLeast reports whether checkout's HEAD descends from
// min. It only reads the repository (merge-base --is-ancestor), with
// ambient GIT_* variables dropped so the query names this checkout.
func goleanCheckoutAtLeast(checkout, min string) error {
	cmd := exec.Command("git", "-C", checkout, "merge-base", "--is-ancestor", min, "HEAD")
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &exit) && exit.ExitCode() == 1:
		return fmt.Errorf("GoLean checkout %s predates the minimum revision %s", checkout, min)
	default:
		return fmt.Errorf("cannot confirm GoLean checkout %s contains the minimum revision %s: %v: %s", checkout, min, err, strings.TrimSpace(string(out)))
	}
}

// resolveGoLeanCheckout picks the checkout for integration tests. An
// explicit GOLEAN_CHECKOUT that is unusable is an error (the caller asked
// for it); the implicit fallback is skipped with the reason instead.
func resolveGoLeanCheckout(env, fallback, min string) (checkout, skip string, err error) {
	explicit := env != ""
	checkout = env
	if !explicit {
		checkout = fallback
	}
	checkout, err = filepath.Abs(checkout)
	if err != nil {
		return "", "", err
	}
	problem := func(err error) (string, string, error) {
		if explicit {
			return "", "", fmt.Errorf("GOLEAN_CHECKOUT: %w", err)
		}
		return "", fmt.Sprintf("%v (set GOLEAN_CHECKOUT to a checkout containing %s)", err, min), nil
	}
	if _, err := os.Stat(filepath.Join(checkout, "scripts", "diff-coverage")); err != nil {
		return problem(fmt.Errorf("GoLean checkout unavailable: %w", err))
	}
	if err := goleanCheckoutAtLeast(checkout, min); err != nil {
		return problem(err)
	}
	return checkout, "", nil
}

func TestResolveGoLeanCheckoutMinimumRevision(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	repo := t.TempDir()
	gitIn := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", repo,
			"-c", "user.name=test", "-c", "user.email=test@example.invalid", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	gitIn("init", "-q")
	if err := os.MkdirAll(filepath.Join(repo, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "scripts", "diff-coverage"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn("add", ".")
	gitIn("commit", "-q", "-m", "old")
	old := gitIn("rev-parse", "HEAD")
	gitIn("commit", "-q", "--allow-empty", "-m", "minimum")
	min := gitIn("rev-parse", "HEAD")
	gitIn("checkout", "-q", old)

	// Too old: the implicit fallback skips naming the minimum; an
	// explicit selection fails.
	if _, skip, err := resolveGoLeanCheckout("", repo, min); err != nil || !strings.Contains(skip, min) {
		t.Fatalf("implicit too-old checkout: skip=%q err=%v", skip, err)
	}
	if _, _, err := resolveGoLeanCheckout(repo, "unused", min); err == nil || !strings.Contains(err.Error(), "predates") {
		t.Fatalf("explicit too-old checkout accepted: %v", err)
	}
	// An unknown minimum (e.g. a shallow clone) is not confirmed either.
	if _, _, err := resolveGoLeanCheckout(repo, "unused", strings.Repeat("0", 40)); err == nil {
		t.Fatal("explicit checkout without the minimum commit accepted")
	}
	gitIn("checkout", "-q", min)
	for _, env := range []string{"", repo} {
		got, skip, err := resolveGoLeanCheckout(env, repo, min)
		if err != nil || skip != "" || got != repo {
			t.Fatalf("current checkout (env %q): got=%q skip=%q err=%v", env, got, skip, err)
		}
	}
	// Missing: skip implicitly, fail explicitly.
	missing := filepath.Join(t.TempDir(), "absent")
	if _, skip, err := resolveGoLeanCheckout("", missing, min); err != nil || skip == "" {
		t.Fatalf("implicit missing checkout: skip=%q err=%v", skip, err)
	}
	if _, _, err := resolveGoLeanCheckout(missing, "unused", min); err == nil {
		t.Fatal("explicit missing checkout accepted")
	}
}
