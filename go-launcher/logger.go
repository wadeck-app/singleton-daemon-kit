package launcher

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	logMu   sync.Mutex
	logFile *os.File
	logDay  string
)

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

// logWrite writes to stderr unconditionally, and additionally to the daily log
// file when configDir is non-empty and the file can be opened.
// NEVER add a separate fmt.Fprintf(os.Stderr, ...) alongside a logInfo/logWarn/logError
// call — that produces duplicate output. Use the log functions exclusively.
func logWrite(configDir, level, category, msg string) {
	logMu.Lock()
	defer logMu.Unlock()
	ts := time.Now().Format("15:04:05")
	line := fmt.Sprintf("[%s] [%5s] [%s] %s\n", ts, level, category, msg)
	if configDir != "" {
		if err := openLogFile(configDir); err == nil && logFile != nil {
			_, _ = logFile.WriteString(line)
		}
	}
	_, _ = fmt.Fprint(os.Stderr, line)
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
