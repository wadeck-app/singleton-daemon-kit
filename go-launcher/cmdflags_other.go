//go:build !windows

package launcher

import "os/exec"

// hasConsole always returns true on non-Windows: no CREATE_NO_WINDOW concept.
func hasConsole() bool { return true }

func setCmdFlags(_ *exec.Cmd) {}
