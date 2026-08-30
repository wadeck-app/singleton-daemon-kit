//go:build windows

package launcher

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// spawnUpdateAndExit spawns UpdateCmd as a detached, hidden process then exits immediately.
// Exiting releases the file lock on the running .exe so npm can overwrite it.
// Only call after validateUpdateCmd has confirmed the command is safe.
func spawnUpdateAndExit(configDir string, cmd []string) {
	fmt.Fprintf(os.Stderr, "Spawning update: %v, exiting to release file lock\n", cmd)
	logInfo(configDir, "launcher", fmt.Sprintf("spawning update command (detached): %v", cmd))

	// Use wscript.exe + VBScript with SW_HIDE=0 — the most reliable way to
	// suppress ALL console windows on Windows, including npm lifecycle scripts.
	// SW_HIDE propagates to every child process spawned by the VBS script.
	vbsPath := filepath.Join(configDir, "update-runner.vbs")
	if err := writeUpdateVbs(vbsPath, cmd); err != nil {
		logError(configDir, "launcher", fmt.Sprintf("failed to write update VBS: %v", err))
		fmt.Fprintf(os.Stderr, "Error: failed to write update VBS: %v\n", err)
		os.Exit(1)
	}
	logInfo(configDir, "launcher", fmt.Sprintf("wrote update VBS: %s", vbsPath))

	c := exec.Command("wscript.exe", "//nologo", vbsPath)
	c.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000 | 0x00000008, // CREATE_NO_WINDOW | DETACHED_PROCESS
	}
	if err := c.Start(); err != nil {
		logError(configDir, "launcher", fmt.Sprintf("failed to spawn wscript: %v", err))
		fmt.Fprintf(os.Stderr, "Error: failed to spawn wscript: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// writeUpdateVbs writes a VBScript that runs the update command with SW_HIDE.
// All child processes spawned by WshShell.Run with nWindowStyle=0 are hidden.
func writeUpdateVbs(vbsPath string, cmd []string) error {
	// Find node.exe + npm-cli.js (preferred over npm.cmd to avoid batch file issues).
	nodePath, _ := exec.LookPath("node")
	npmCli := ""
	if nodePath != "" {
		candidate := filepath.Join(filepath.Dir(nodePath), "node_modules", "npm", "bin", "npm-cli.js")
		if _, err := os.Stat(candidate); err == nil {
			npmCli = candidate
		}
	}

	var vbsCmd string
	if nodePath != "" && npmCli != "" {
		// node npm-cli.js install -g <pkg>  (cmd[0]="npm" is replaced by node+npmCli)
		parts := []string{quoteVbs(nodePath), quoteVbs(npmCli)}
		parts = append(parts, quoteVbsSlice(cmd[1:])...)
		vbsCmd = strings.Join(parts, " ")
	} else {
		// Fallback: npm.cmd via cmd.exe
		parts := []string{`cmd.exe`, `/C`, `npm`}
		parts = append(parts, cmd[1:]...)
		vbsCmd = strings.Join(quoteVbsSlice(parts), " ")
	}

	// VBScript: WshShell.Run cmd, 0=SW_HIDE, False=don't wait
	vbs := fmt.Sprintf(`Dim oShell
Set oShell = CreateObject("WScript.Shell")
oShell.Run "%s", 0, False
`, strings.ReplaceAll(vbsCmd, `"`, `""`))

	return os.WriteFile(vbsPath, []byte(vbs), 0o600)
}

func quoteVbs(s string) string {
	return `"` + strings.ReplaceAll(s, `\`, `\\`) + `"`
}

func quoteVbsSlice(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = quoteVbs(a)
	}
	return out
}
