//go:build windows

package launcher

import (
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// hasConsole reports whether the launcher has a usable stdout handle (pipe or
// character device). Used to decide whether to forward real stdio to node or
// open NUL handles. Note: a SUBSYSTEM:WINDOWS process can never have an
// attached console — GetConsoleMode always fails — but it may have valid piped
// stdio handles inherited from the caller.
func hasConsole() bool {
	var consoleMode uint32
	if syscall.GetConsoleMode(syscall.Handle(os.Stdout.Fd()), &consoleMode) == nil {
		return true
	}
	ft, _ := windows.GetFileType(windows.Handle(os.Stdout.Fd()))
	return ft != 0
}

func setCmdFlags(cmd *exec.Cmd) {
	// This binary is compiled as SUBSYSTEM:WINDOWS (GUI). A GUI process NEVER
	// has an attached console, so GetConsoleMode always fails regardless of the
	// inherited stdio handles. Spawning a SUBSYSTEM:CONSOLE process (node.exe)
	// from a GUI parent without CREATE_NO_WINDOW causes Windows to allocate a
	// new visible console window for the child — the terminal flash the user sees.
	//
	// With CREATE_NO_WINDOW, the child inherits the pipe/file handles normally
	// (WriteFile still works on any valid HANDLE), so output still flows back to
	// the caller via the piped stdio. WriteConsoleW is never needed because there
	// is no attached console in a SUBSYSTEM:WINDOWS process tree.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
}
