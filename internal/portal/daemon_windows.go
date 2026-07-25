//go:build windows

package portal

import (
	"os/exec"
	"strconv"
	"syscall"
)

// setDetached starts the browser in a new process group so it outlives gongctl's
// exit (Windows). 0x00000200 = CREATE_NEW_PROCESS_GROUP.
func setDetached(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000200}
}

// killTree ends the browser process and its children (Windows).
func killTree(pid int) {
	if pid <= 0 {
		return
	}
	exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid)).Run()
}
