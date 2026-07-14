//go:build windows

package launcher

import (
	"os"
	"os/exec"
	"os/signal"
)

func forwardSignals(cmd *exec.Cmd) chan struct{} {
	sigCh := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(sigCh, os.Interrupt)
	go func() {
		defer close(done)
		select {
		case <-sigCh:
			if cmd.Process != nil {
				_ = cmd.Process.Signal(os.Interrupt)
			}
		case <-done:
		}
		signal.Stop(sigCh)
	}()
	return done
}
