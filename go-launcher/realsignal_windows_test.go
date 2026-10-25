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
		defer child.Process.Kill()
		signalReceived.Store(false)
		forwardSignals(child)
		fmt.Println("ready")
		deadline := time.Now().Add(10 * time.Second)
		for !signalReceived.Load() && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		fmt.Println("signalReceived=", signalReceived.Load())
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

func TestSleepOrSignal_aRealConsoleInterruptCutsTheSleepShort(t *testing.T) {
	start := time.Now()
	if got := runHelper(t, "sleep"); !strings.Contains(got, "interrupted= true") {
		t.Errorf("sleepOrSignal was not interrupted, helper said: %q", got)
	}
	if time.Since(start) > 8*time.Second {
		t.Errorf("the sleep ran its course instead of being cut short")
	}
}
