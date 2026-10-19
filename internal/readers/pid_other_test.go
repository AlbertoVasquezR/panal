//go:build !windows

package readers

import (
	"os"
	"testing"
)

// pid 1 always exists; run as a normal user, signalling it is EPERM.
func TestRealPIDAliveCountsOtherUsersProcesses(t *testing.T) {
	if !realPIDAlive(1) {
		t.Fatal("pid 1 should count as alive")
	}
	if !realPIDAlive(os.Getpid()) {
		t.Fatal("own pid should count as alive")
	}
}
