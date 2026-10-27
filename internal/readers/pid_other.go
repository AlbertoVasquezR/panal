//go:build !windows

package readers

import (
	"errors"
	"os"
	"syscall"
)

func realPIDAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// EPERM: it exists but belongs to another user (a delegate run with sudo).
	err = p.Signal(syscall.Signal(0))
	return err == nil || errors.Is(err, syscall.EPERM)
}
