//go:build !unix

package runner

import (
	"os"
	"syscall"
)

// newProcessGroup has no portable equivalent outside Unix. bash-teacher does
// not support such platforms in v1; the runner degrades to killing only the
// shell it started.
func newProcessGroup() *syscall.SysProcAttr { return nil }

func killGroup(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}
