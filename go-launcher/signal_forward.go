package launcher

import (
	"fmt"
	"os"
)

// deliverStop hands sig to the daemon through send (cmd.Process.Signal in production) and reports a failure through warn.
//
// The error used to be dropped. On Windows it is the normal outcome, not a corner case: Go implements Process.Signal
// there for os.Kill only and returns "not supported by windows" for os.Interrupt, so an interrupt was never delivered
// and nobody was told. The daemon is not stopped in that case and the launcher keeps waiting for it (runDaemonOnce is
// blocked in cmd.Wait), so the message says so and names the way that does work.
func deliverStop(sig os.Signal, send func(os.Signal) error, warn func(string)) {
	if err := send(sig); err != nil {
		warn(fmt.Sprintf("could not forward the %s signal to the daemon: %v; the daemon is still running and the launcher "+
			"keeps waiting for it: stop it with its own quit command (the HTTP quit, e.g. the app's stop/quit command), "+
			"not with a signal", sig, err))
	}
}
