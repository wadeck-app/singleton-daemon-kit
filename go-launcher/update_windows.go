//go:build windows

package launcher

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// spawnUpdateAndExit spawns UpdateCmd as a detached, hidden process then exits immediately.
// Exiting releases the file lock on the running .exe so npm can overwrite it.
// Only call after validateUpdateCmd has confirmed the command is safe.
func spawnUpdateAndExit(configDir string, cmd []string) {
	fmt.Fprintf(os.Stderr, "Spawning update: %v, exiting to release file lock\n", cmd)
	logInfo(configDir, "launcher", fmt.Sprintf("spawning update command (detached): %v", cmd))

	// Run via Node.js + npm-cli.js to avoid cmd.exe/.cmd batch files.
	// cmd.exe spawns multiple console-visible sub-processes for npm.cmd → 20-40 terminal flashes.
	// Using `node npm-cli.js` directly keeps everything in a single hidden Node process.
	nodePath, nodeErr := exec.LookPath("node")
	npmCliPath := ""
	if nodeErr == nil {
		npmCliPath = filepath.Join(filepath.Dir(nodePath), "node_modules", "npm", "bin", "npm-cli.js")
		if _, err := os.Stat(npmCliPath); err != nil {
			npmCliPath = ""
		}
	}

	var c *exec.Cmd
	if npmCliPath != "" {
		// Preferred: node npm-cli.js install -g <pkg> — no cmd.exe, no console popups.
		nodeArgs := append([]string{npmCliPath}, cmd[1:]...) // drop "npm", keep "install -g <pkg>"
		c = exec.Command(nodePath, nodeArgs...)
		logInfo(configDir, "launcher", fmt.Sprintf("using node+npm-cli.js: %s %v", nodePath, nodeArgs))
	} else {
		// Fallback: PowerShell with hidden window — safer than cmd.exe for batch files.
		psCmd := ""
		for i, a := range cmd {
			if i > 0 {
				psCmd += " "
			}
			psCmd += a
		}
		c = exec.Command("powershell", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", psCmd)
		logInfo(configDir, "launcher", fmt.Sprintf("fallback: powershell -Command %s", psCmd))
	}

	c.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000 | 0x00000008, // CREATE_NO_WINDOW | DETACHED_PROCESS
	}
	if err := c.Start(); err != nil {
		logError(configDir, "launcher", fmt.Sprintf("failed to spawn update command: %v", err))
		fmt.Fprintf(os.Stderr, "Error: failed to spawn update command: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}
