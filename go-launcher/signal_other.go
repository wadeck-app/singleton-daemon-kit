//go:build !windows

package launcher

import (
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

// stopSignals are the signals that ask the launcher to stop.
var stopSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}

// forwardSignals starts a goroutine that forwards os.Interrupt/SIGTERM to cmd.
// Returns a stop channel: the caller must close it (exactly once) to
// signal the goroutine to exit. The goroutine never closes this channel —
// doing so would cause a panic when the caller also closes it (e.g. after
// cmd.Wait() returns in runDaemon).
func forwardSignals(cmd *exec.Cmd, warn func(string)) chan struct{} {
	sigCh := make(chan os.Signal, 1)
	stop := make(chan struct{})
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		defer signal.Stop(sigCh)
		select {
		case sig := <-sigCh:
			signalReceived.Store(true)
			if cmd.Process != nil {
				deliverStop(sig, cmd.Process.Signal, warn)
			}
		case <-stop:
		}
	}()
	return stop
}
