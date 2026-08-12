//go:build windows

package launcher

import (
	"os"
	"os/exec"
	"os/signal"
)

// SIGTERM is not a real signal on Windows — os.Interrupt (Ctrl+C / SIGBREAK)
// is the only portable graceful-stop signal. signal_other.go handles SIGTERM
// for Linux/macOS. Do not add SIGTERM here.

// forwardSignals starts a goroutine that forwards os.Interrupt to cmd.
// Returns a stop channel: the caller must close it (exactly once) to
// signal the goroutine to exit. The goroutine never closes this channel —
// doing so would cause a panic when the caller also closes it (e.g. after
// cmd.Wait() returns in runDaemon).
func forwardSignals(cmd *exec.Cmd) chan struct{} {
	sigCh := make(chan os.Signal, 1)
	stop := make(chan struct{})
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		defer signal.Stop(sigCh)
		select {
		case <-sigCh:
			if cmd.Process != nil {
				_ = cmd.Process.Signal(os.Interrupt)
			}
		case <-stop:
		}
	}()
	return stop
}
