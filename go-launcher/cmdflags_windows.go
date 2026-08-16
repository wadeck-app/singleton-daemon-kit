//go:build windows

package launcher

import (
	"os"
	"os/exec"
	"syscall"
)

func setCmdFlags(cmd *exec.Cmd) {
	// CREATE_NO_WINDOW prevents a console popup when the launcher is started
	// without a terminal (e.g. from a desktop shortcut or Task Scheduler).
	// When the launcher itself is already running inside a console (stdout is
	// a console screen buffer), CREATE_NO_WINDOW would break child process
	// stdout: node.exe uses WriteConsoleW, which silently fails when the
	// process has no attached console even if the handle is valid.
	// Solution: only set CREATE_NO_WINDOW when the launcher has no console.
	var consoleMode uint32
	hasConsole := syscall.GetConsoleMode(syscall.Handle(os.Stdout.Fd()), &consoleMode) == nil

	var creationFlags uint32
	if !hasConsole {
		creationFlags = 0x08000000 // CREATE_NO_WINDOW
	}

	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: creationFlags,
	}
}
