//go:build !windows

package portal

import (
	"os/exec"
	"syscall"
	"time"
)

// setDetached puts the launched browser in its own process group so it outlives
// gongctl's exit (Unix).
func setDetached(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killTree ends the browser's whole process group (launchBrowser made it a group
// leader via Setpgid), so Chrome's children go with it. SIGTERM first so the
// profile is flushed, SIGKILL only if it refuses.
func killTree(pid int) {
	if pid <= 0 {
		return
	}
	syscall.Kill(-pid, syscall.SIGTERM)
	time.Sleep(700 * time.Millisecond)
	syscall.Kill(-pid, syscall.SIGKILL)
}
