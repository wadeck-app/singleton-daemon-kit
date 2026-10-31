//go:build windows

package launcher

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Real console events, not a fake: the parent sends a child copy of this test binary the control event Ctrl+Break
// produces, which Go turns into os.Interrupt. CTRL_BREAK is used because it can be aimed at one process group; CTRL_C
// cannot (it goes to every process of the console, the test runner included). It needs a console attached to `go test`.

const helperEnv = "LAUNCHER_SIGNAL_HELPER"

// TestHelperProcess is the child side; without the env var it is a no-op.
func TestHelperProcess(t *testing.T) {
	mode := os.Getenv(helperEnv)
	if mode == "" {
		return
	}
	switch mode {
	case "forward":
		child := exec.Command("ping", "-n", "30", "127.0.0.1")
		if err := child.Start(); err != nil {
			fmt.Println("helper: cannot start the child:", err)
			os.Exit(2)
		}
		// Not a defer: the helper leaves through os.Exit below, which skips deferred calls and left this ping running for 30 s.
		stopChild := func() {
			_ = child.Process.Kill()
			_ = child.Wait()
		}
		signalReceived.Store(false)
		// The warning is the last thing forwardSignals does with an interrupt (signalReceived is set before the forward is
		// attempted), so waiting for it, not for signalReceived, avoids reading the state too early without any sleep.
		warned := make(chan string, 1)
		forwardSignals(child, func(m string) { warned <- m })
		fmt.Println("ready")
		select {
		case m := <-warned:
			fmt.Println("warning=", m)
		case <-time.After(10 * time.Second):
			fmt.Println("warning= none within 10s")
		}
		fmt.Println("signalReceived=", signalReceived.Load())
		fmt.Println("child=", child.Process.Pid)
		stopChild()
	case "sleep":
		// "ready" can only be printed from inside the sleep, so it comes after the handler is installed: 300ms is far above
		// the microseconds sleepOrSignal needs to call signal.Notify.
		go func() {
			time.Sleep(300 * time.Millisecond)
			fmt.Println("ready")
		}()
		fmt.Println("interrupted=", sleepOrSignal(10*time.Second))
	}
	os.Exit(0)
}

// runHelper starts the helper in its own process group, waits until it is ready, sends it CTRL_BREAK and returns what it printed after.
func runHelper(t *testing.T, mode string) string {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$")
	cmd.Env = append(os.Environ(), helperEnv+"="+mode)
	// CREATE_NEW_PROCESS_GROUP: the helper's pid is the group id CTRL_BREAK is aimed at.
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000200}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	r := bufio.NewReader(out)
	if line, err := r.ReadString('\n'); err != nil || strings.TrimSpace(line) != "ready" {
		t.Fatalf("helper did not become ready: %q (%v)", line, err)
	}
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GenerateConsoleCtrlEvent")
	// 1 = CTRL_BREAK_EVENT
	if ok, _, callErr := proc.Call(1, uintptr(cmd.Process.Pid)); ok == 0 {
		t.Fatalf("GenerateConsoleCtrlEvent failed (a console must be attached to go test): %v", callErr)
	}
	rest, _ := io.ReadAll(r)
	_ = cmd.Wait()
	return string(rest)
}

func TestForwardSignals_aRealConsoleInterruptSetsSignalReceived(t *testing.T) {
	if got := runHelper(t, "forward"); !strings.Contains(got, "signalReceived= true") {
		t.Errorf("forwardSignals did not record the interrupt, helper said: %q", got)
	}
}

// Go cannot deliver an interrupt to another process on Windows: the failure used to be dropped, now it is a warning that
// says the daemon is still running and how to stop it.
func TestForwardSignals_aRealConsoleInterruptThatCannotBeForwardedIsWarned(t *testing.T) {
	got := runHelper(t, "forward")
	for _, want := range []string{"warning= could not forward the interrupt signal", "not supported by windows", "still running", "quit"} {
		if !strings.Contains(got, want) {
			t.Errorf("the helper output lacks %q: %q", want, got)
		}
	}
}

// processAlive reports whether pid still names a running process (a handle can be opened and it has not exited).
func processAlive(t *testing.T, pid int) bool {
	t.Helper()
	const synchronize, processQueryLimitedInformation, stillActive = 0x00100000, 0x1000, 259
	h, err := syscall.OpenProcess(synchronize|processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		// ERROR_INVALID_PARAMETER: no such process any more.
		return false
	}
	defer syscall.CloseHandle(h)
	var code uint32
	if err := syscall.GetExitCodeProcess(h, &code); err != nil {
		t.Fatalf("GetExitCodeProcess(%d): %v", pid, err)
	}
	return code == stillActive
}

// The helper starts a `ping -n 30` child; it used to exit through os.Exit with the kill deferred, so the child outlived
// the test by about 30 s. The helper reports the child pid, and the child must be gone once the helper has exited.
func TestForwardSignals_theHelperDoesNotLeakItsChildProcess(t *testing.T) {
	got := runHelper(t, "forward")
	var pid int
	for _, line := range strings.Split(got, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "child="); ok {
			if _, err := fmt.Sscan(v, &pid); err != nil {
				t.Fatalf("unreadable child pid in %q: %v", line, err)
			}
		}
	}
	if pid == 0 {
		t.Fatalf("the helper did not report its child pid, so the leak cannot be checked: %q", got)
	}
	if processAlive(t, pid) {
		// Do not let the failing test leak it too.
		if p, err := os.FindProcess(pid); err == nil {
			_ = p.Kill()
		}
		t.Errorf("the helper's child process %d is still running after the helper exited", pid)
	}
}

func TestSleepOrSignal_aRealConsoleInterruptCutsTheSleepShort(t *testing.T) {
	start := time.Now()
	if got := runHelper(t, "sleep"); !strings.Contains(got, "interrupted= true") {
		t.Errorf("sleepOrSignal was not interrupted, helper said: %q", got)
	}
	if time.Since(start) > 8*time.Second {
		t.Errorf("the sleep ran its course instead of being cut short")
	}
}
