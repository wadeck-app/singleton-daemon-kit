//go:build !windows

package launcher

import (
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

// Real signals: the test process sends itself SIGTERM. A handler of its own is registered first so that a signal that
// the code under test missed fails the assertion instead of terminating the test binary.

func TestForwardSignals_aRealSIGTERMSetsSignalReceivedAndReachesTheChild(t *testing.T) {
	guard := make(chan os.Signal, 1)
	signal.Notify(guard, syscall.SIGTERM)
	defer signal.Stop(guard)

	signalReceived.Store(false)
	child := exec.Command("sleep", "30")
	if err := child.Start(); err != nil {
		t.Fatalf("cannot start `sleep`: %v", err)
	}
	waited := make(chan error, 1)
	go func() { waited <- child.Wait() }()

	stop := forwardSignals(child)
	defer close(stop)
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-waited:
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("the child ended without being signalled: %v", err)
		}
		if ws, ok := exitErr.Sys().(syscall.WaitStatus); !ok || !ws.Signaled() || ws.Signal() != syscall.SIGTERM {
			t.Errorf("the child was not terminated by the forwarded SIGTERM: %v", exitErr)
		}
	case <-time.After(5 * time.Second):
		_ = child.Process.Kill()
		t.Fatal("SIGTERM was not forwarded to the child within 5s")
	}
	if !signalReceived.Load() {
		t.Errorf("signalReceived was not set")
	}
}

func TestSleepOrSignal_aRealSIGTERMCutsTheSleepShort(t *testing.T) {
	guard := make(chan os.Signal, 1)
	signal.Notify(guard, syscall.SIGTERM)
	defer signal.Stop(guard)

	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
	}()
	start := time.Now()
	if !sleepOrSignal(10 * time.Second) {
		t.Errorf("sleepOrSignal did not report the signal")
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("the sleep ran its course instead of being cut short")
	}
}

// Death by SIGKILL (what the OOM killer sends) must come back from the real runner as a non-zero code the supervisor
// restarts on, with no signal of the launcher's own recorded.
func TestRunDaemonOnce_aSIGKILLedDaemonIsReportedAsANonZeroUnsignalledExit(t *testing.T) {
	res := runNodeScript(t, "process.kill(process.pid, 'SIGKILL');")
	if res.code != -1 {
		t.Errorf("exit code = %d, want -1 (Go's ExitCode for a process killed by a signal)", res.code)
	}
	if res.signalled {
		t.Errorf("signalled must be false: the launcher forwarded nothing")
	}
}
