//go:build !windows

package launcher

import (
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

func forwardSignals(cmd *exec.Cmd) chan struct{} {
	sigCh := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		defer close(done)
		select {
		case sig := <-sigCh:
			if cmd.Process != nil {
				_ = cmd.Process.Signal(sig)
			}
		case <-done:
		}
		signal.Stop(sigCh)
	}()
	return done
}
