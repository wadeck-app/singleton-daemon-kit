package launcher

import (
	"fmt"
	"os"
	"path/filepath"
)

// formatDaemonNotRunning returns a user-friendly message when the daemon is not running.
func formatDaemonNotRunning(configDir string) string {
	return fmt.Sprintf(
		"wdrive daemon is not running.\nStart it with: wdrive\nLog directory: %s/",
		filepath.Join(configDir, "logs"),
	)
}

// formatDaemonNotResponding returns a user-friendly message when the daemon does not respond.
func formatDaemonNotResponding(port int, configDir string) string {
	return fmt.Sprintf(
		"wdrive daemon is not responding (port %d).\nThe daemon may have crashed. Check logs: %s/\nTo restart: wdrive",
		port,
		filepath.Join(configDir, "logs"),
	)
}

// formatHTTPError returns a status-specific user-friendly message for non-200 HTTP responses.
func formatHTTPError(statusCode int, command, configDir string) string {
	logsDir := filepath.Join(configDir, "logs") + "/"
	switch statusCode {
	case 401:
		return "Authentication error — health token mismatch. Restart wdrive."
	case 404:
		return fmt.Sprintf("Unknown command '%s'. Run wdrive --help for available commands.", command)
	case 500:
		return fmt.Sprintf("wdrive daemon returned an internal error. Check logs: %s", logsDir)
	default:
		return fmt.Sprintf("Unexpected response from daemon (status %d). Check logs: %s", statusCode, logsDir)
	}
}

// formatNodeStartError returns a user-friendly message when node fails to start.
func formatNodeStartError(nodeScript string, err error) string {
	return fmt.Sprintf(
		"Failed to start %s: %s\nCheck that %s exists and node is in PATH.",
		filepath.Base(nodeScript),
		err.Error(),
		nodeScript,
	)
}

// formatSentinelReadError returns a user-friendly message for a sentinel read error.
// When err is ENOENT (normal clean exit), returns "Exiting cleanly" message.
// For other errors, returns the error with the sentinel path.
func formatSentinelReadError(sentinelPath string, err error) string {
	if os.IsNotExist(err) {
		return "Exiting cleanly — no restart requested."
	}
	return fmt.Sprintf("Could not read restart sentinel (%s): %v", sentinelPath, err)
}

// writeLauncherPIDFile writes the launcher PID to <configDir>/config.launcher-pid.
func writeLauncherPIDFile(configDir string, pid int) error {
	pidPath := filepath.Join(configDir, "config.launcher-pid")
	return os.WriteFile(pidPath, []byte(fmt.Sprintf("%d", pid)), 0o644)
}
