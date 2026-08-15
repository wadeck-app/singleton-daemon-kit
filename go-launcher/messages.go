package launcher

import (
	"fmt"
	"os"
	"path/filepath"
)

// formatDaemonNotRunning returns a user-friendly message when the daemon is not running.
func formatDaemonNotRunning(app, configDir string) string {
	return fmt.Sprintf(
		"%s daemon is not running.\nStart it with: %s\nLog directory: %s/",
		app, app,
		filepath.Join(configDir, "logs"),
	)
}

// formatDaemonNotResponding returns a user-friendly message when the daemon does not respond.
func formatDaemonNotResponding(app string, port int, configDir string) string {
	return fmt.Sprintf(
		"%s daemon is not responding (port %d).\nThe daemon may have crashed. Check logs: %s/\nTo restart: %s",
		app, port,
		filepath.Join(configDir, "logs"),
		app,
	)
}

// formatHTTPError returns a status-specific user-friendly message for non-200 HTTP responses.
func formatHTTPError(app string, statusCode int, command, configDir string) string {
	logsDir := filepath.Join(configDir, "logs") + "/"
	switch statusCode {
	case 401:
		return fmt.Sprintf("Authentication error — health token mismatch. Restart %s.", app)
	case 404:
		return fmt.Sprintf("Unknown command '%s'. Run %s --help for available commands.", command, app)
	case 500:
		return fmt.Sprintf("%s daemon returned an internal error. Check logs: %s", app, logsDir)
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

