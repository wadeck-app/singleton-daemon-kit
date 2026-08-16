package launcher

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	logMu      sync.Mutex
	logFile    *os.File
	logDay     string
	silentMode bool
)

// setSilentMode suppresses INFO/WARN launcher logs from stderr when enabled.
// ERROR logs are always written to stderr regardless. File logging is unaffected.
func setSilentMode(v bool) {
	logMu.Lock()
	silentMode = v
	logMu.Unlock()
}

func openLogFile(configDir string) error {
	day := time.Now().Format("2006-01-02")
	if logFile != nil && logDay == day {
		return nil
	}
	if logFile != nil {
		_ = logFile.Close()
	}
	logsDir := filepath.Join(configDir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return err
	}
	name := filepath.Join(logsDir, day+"-launcher.log")
	f, err := os.OpenFile(name, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	logFile = f
	logDay = day
	return nil
}

// logWrite writes to the daily log file (always) and to stderr (unless silentMode
// is active and the level is INFO or WARN).
// NEVER add a separate fmt.Fprintf(os.Stderr, ...) alongside a logInfo/logWarn/logError
// call — that produces duplicate output. Use the log functions exclusively.
// writeFallbackLog writes to a temp-dir fallback file when normal logging fails.
// Used when configDir is empty or openLogFile returns an error.
func writeFallbackLog(line string) {
	name := filepath.Join(os.TempDir(), "wdrive-launcher-fallback.log")
	if f, err := os.OpenFile(name, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		_, _ = f.WriteString(line)
		_ = f.Close()
	}
}

func logWrite(configDir, level, category, msg string) {
	logMu.Lock()
	defer logMu.Unlock()
	ts := time.Now().Format("15:04:05")
	line := fmt.Sprintf("[%s] [%5s] [%s] %s\n", ts, level, category, msg)
	if configDir != "" {
		if err := openLogFile(configDir); err == nil && logFile != nil {
			_, _ = logFile.WriteString(line)
		} else if err != nil {
			// Never silently drop log entries — write to fallback so failures are diagnosable.
			writeFallbackLog(fmt.Sprintf("openLogFile failed (configDir=%q): %v\n%s", configDir, err, line))
		}
	} else {
		// No configDir: write to fallback so headless launches without a config are diagnosable.
		writeFallbackLog(line)
	}
	// In silent mode, suppress INFO and WARN from stderr so short-lived commands
	// (--help, --version, --pid) are not buried in launcher lifecycle noise.
	if !silentMode || level == "ERROR" {
		_, _ = fmt.Fprint(os.Stderr, line)
	}
}

func logInfo(configDir, category, msg string) {
	logWrite(configDir, " INFO", category, msg)
}

func logWarn(configDir, category, msg string) {
	logWrite(configDir, " WARN", category, msg)
}

func logError(configDir, category, msg string) {
	logWrite(configDir, "ERROR", category, msg)
}
