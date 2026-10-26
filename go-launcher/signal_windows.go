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

// stopSignals are the signals that ask the launcher to stop.
var stopSignals = []os.Signal{os.Interrupt}

// forwardSignals starts a goroutine that tries to forward os.Interrupt to cmd. On Windows this cannot work: Go only
// implements Process.Signal for os.Kill, so the failure is passed to warn, the daemon keeps running and the launcher
// keeps waiting for it. The OS does not deliver a console Ctrl+C to the daemon either: it is started with
// CREATE_NO_WINDOW, which gives it a console of its own, and the shipped launcher is a GUI process with no console.
// The stop that works is the daemon's own quit (HTTP), which the warning names.
// Returns a stop channel: the caller must close it (exactly once) to
// signal the goroutine to exit. The goroutine never closes this channel —
// doing so would cause a panic when the caller also closes it (e.g. after
// cmd.Wait() returns in runDaemon).
func forwardSignals(cmd *exec.Cmd, warn func(string)) chan struct{} {
	sigCh := make(chan os.Signal, 1)
	stop := make(chan struct{})
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		defer signal.Stop(sigCh)
		select {
		case <-sigCh:
			signalReceived.Store(true)
			if cmd.Process != nil {
				deliverStop(os.Interrupt, cmd.Process.Signal, warn)
			}
		case <-stop:
		}
	}()
	return stop
}
