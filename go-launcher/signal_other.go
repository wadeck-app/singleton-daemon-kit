//go:build !windows

package launcher

import (
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

// forwardSignals starts a goroutine that forwards os.Interrupt/SIGTERM to cmd.
// Returns a stop channel: the caller must close it (exactly once) to
// signal the goroutine to exit. The goroutine never closes this channel —
// doing so would cause a panic when the caller also closes it (e.g. after
// cmd.Wait() returns in runDaemon).
func forwardSignals(cmd *exec.Cmd) chan struct{} {
	sigCh := make(chan os.Signal, 1)
	stop := make(chan struct{})
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		defer signal.Stop(sigCh)
		select {
		case sig := <-sigCh:
			if cmd.Process != nil {
				_ = cmd.Process.Signal(sig)
			}
		case <-stop:
		}
	}()
	return stop
}
