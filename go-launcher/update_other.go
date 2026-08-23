//go:build !windows

package launcher

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// spawnUpdateAndExit spawns UpdateCmd in a new session (Setsid) then exits immediately.
// On non-Windows platforms there is no file-lock concern, but the pattern is kept
// consistent so the same Node-side sentinel flow works across all platforms.
// Only call after validateUpdateCmd has confirmed the command is safe.
func spawnUpdateAndExit(configDir string, cmd []string) {
	fmt.Fprintf(os.Stderr, "Spawning update: %v, exiting to release file lock\n", cmd)
	logInfo(configDir, "launcher", fmt.Sprintf("spawning update command (detached): %v", cmd))

	c := exec.Command(cmd[0], cmd[1:]...)
	c.SysProcAttr = &syscall.SysProcAttr{
		// Setsid detaches the child into its own session so it outlives the launcher.
		Setsid: true,
	}
	if err := c.Start(); err != nil {
		logError(configDir, "launcher", fmt.Sprintf("failed to spawn update command %v: %v", cmd, err))
		fmt.Fprintf(os.Stderr, "Error: failed to spawn update command %v: %v\n", cmd, err)
		os.Exit(1)
	}
	os.Exit(0)
}
