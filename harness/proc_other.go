//go:build !unix

package harness

import (
	"os/exec"
	"time"
)

// Off unix there is no process group to kill (job objects are the
// Windows analogue — roadmap territory; the conformance targets run on
// unix), so cancellation falls back to killing the direct child. The
// WaitDelay backstop IS portable and stays: without it a child holding
// the pipes makes Wait block past any deadline (arc-end review: the
// old no-op dropped both).
func killGroup(cmd *exec.Cmd) {
	cmd.WaitDelay = 5 * time.Second
}

// KillGroup is the exported form for sibling adapters.
func KillGroup(cmd *exec.Cmd) { killGroup(cmd) }
