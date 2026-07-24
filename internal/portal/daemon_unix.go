//go:build !windows

package portal

import (
	"os/exec"
	"syscall"
)

// setDetached puts the launched browser in its own process group so it outlives
// gongctl's exit (Unix).
func setDetached(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
