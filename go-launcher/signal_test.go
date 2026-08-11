//go:build windows

package launcher

import (
	"os/exec"
	"testing"
	"time"
)

func TestForwardSignals_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("forwardSignals panicked: %v", r)
		}
	}()

	cmd := exec.Command("cmd", "/c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Skip("cannot start cmd.exe:", err)
	}

	stop := forwardSignals(cmd)
	time.Sleep(20 * time.Millisecond)
	close(stop)
	_ = cmd.Wait()
}
