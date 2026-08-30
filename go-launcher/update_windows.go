//go:build windows

package launcher

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// spawnUpdateAndExit spawns UpdateCmd as a detached, hidden process then exits immediately.
// Exiting releases the file lock on the running .exe so npm can overwrite it.
// Only call after validateUpdateCmd has confirmed the command is safe.
func spawnUpdateAndExit(configDir string, cmd []string) {
	fmt.Fprintf(os.Stderr, "Spawning update: %v, exiting to release file lock\n", cmd)
	logInfo(configDir, "launcher", fmt.Sprintf("spawning update command (detached): %v", cmd))

	// On Windows, npm is a .cmd batch file that must be run through cmd.exe.
	// Directly spawning npm.cmd without a shell causes a silent failure with DETACHED_PROCESS.
	cmdArgs := append([]string{"/C"}, cmd...)
	c := exec.Command("cmd", cmdArgs...)
	c.SysProcAttr = &syscall.SysProcAttr{
		// HideWindow prevents a console popup when spawned from a windowless process.
		HideWindow: true,
		// CREATE_NO_WINDOW (0x08000000) suppresses console creation for cmd.exe, npm and
		// all child processes that would otherwise flash a terminal window.
		// DETACHED_PROCESS (0x00000008) ensures no console handle is inherited.
		CreationFlags: 0x08000000 | 0x00000008,
	}
	if err := c.Start(); err != nil {
		logError(configDir, "launcher", fmt.Sprintf("failed to spawn update command %v: %v", cmd, err))
		fmt.Fprintf(os.Stderr, "Error: failed to spawn update command %v: %v\n", cmd, err)
		os.Exit(1)
	}
	os.Exit(0)
}
