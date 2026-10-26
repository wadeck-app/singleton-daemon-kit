package launcher

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// A stop signal that cannot be handed to the daemon must be reported, not dropped: the launcher then keeps waiting for the
// daemon, and the person who pressed Ctrl+C has to be told why nothing stopped and what to do instead.
func TestDeliverStop_aFailedForwardIsWarnedWithTheCauseAndWhatToDo(t *testing.T) {
	var warnings []string
	deliverStop(os.Interrupt, func(os.Signal) error { return errors.New("not supported by windows") },
		func(m string) { warnings = append(warnings, m) })

	if len(warnings) != 1 {
		t.Fatalf("want exactly one warning, got %d: %q", len(warnings), warnings)
	}
	for _, want := range []string{"interrupt", "not supported by windows", "still running", "quit"} {
		if !strings.Contains(warnings[0], want) {
			t.Errorf("the warning lacks %q: %s", want, warnings[0])
		}
	}
}

func TestDeliverStop_aSuccessfulForwardIsSilentAndSendsTheSignalGiven(t *testing.T) {
	var sent os.Signal
	deliverStop(os.Interrupt, func(s os.Signal) error { sent = s; return nil },
		func(m string) { t.Errorf("unexpected warning: %s", m) })
	if sent != os.Interrupt {
		t.Errorf("sent %v, want the interrupt", sent)
	}
}
