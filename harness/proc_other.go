//go:build !unix

package harness

import "os/exec"

// killGroup is a no-op off unix: cancellation falls back to killing the
// direct child (job objects are the Windows analogue — roadmap
// territory; the conformance targets run on unix).
func killGroup(cmd *exec.Cmd) {}

// KillGroup is the exported form for sibling adapters.
func KillGroup(cmd *exec.Cmd) {}
