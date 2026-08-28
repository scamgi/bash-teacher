//go:build unix

package runner

import (
	"errors"
	"syscall"
)

// newProcessGroup puts the sandboxed shell in its own process group, so the
// whole pipeline — including any children it spawned — can be killed together.
func newProcessGroup() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// killGroup sends SIGKILL to the entire process group. SIGKILL rather than
// SIGTERM because a run that reached the timeout has already had its chance,
// and a handler must not be able to keep the group alive.
//
// A group that has already exited is not an error: the race between the timer
// firing and the last child dying is expected and common.
func killGroup(pid int) error {
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}
