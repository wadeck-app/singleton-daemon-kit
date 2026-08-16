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

func TestAssignJobObject_flagsAcceptedByWindows(t *testing.T) {
	// Regression test: verifies that SetInformationJobObject succeeds with our
	// chosen limit flags. The original bug used 0x00000100 (DIE_ON_UNHANDLED_EXCEPTION)
	// instead of 0x00001000 (JOB_OBJECT_LIMIT_SILENT_BREAKAWAY_OK), causing
	// SetInformationJobObject to fail silently — the Job Object was created but had
	// NO restrictions, so KILL_ON_JOB_CLOSE did not apply.
	cmd := exec.Command("cmd", "/c", "exit 0")
	if err := cmd.Start(); err != nil {
		t.Skip("cannot start child process:", err)
	}
	defer cmd.Wait()

	if err := assignJobObject(cmd); err != nil {
		t.Errorf("assignJobObject failed — likely wrong LimitFlags constant: %v", err)
	}
}
