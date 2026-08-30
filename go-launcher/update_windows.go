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

	c := exec.Command(cmd[0], cmd[1:]...)
	c.SysProcAttr = &syscall.SysProcAttr{
		// HideWindow prevents a console popup when spawned from a windowless process.
		HideWindow: true,
		// CREATE_NO_WINDOW (0x08000000) suppresses console creation for npm and all its
		// child processes (node scripts, etc.) that would otherwise flash a terminal window.
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
