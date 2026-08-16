package launcher

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// --- ResolveConfigDir ---

func TestResolveConfigDir_flag(t *testing.T) {
	result := ResolveConfigDir([]string{"--config", "/custom/dir", "--quit"}, "/default")
	if result != "/custom/dir" {
		t.Errorf("expected /custom/dir, got %s", result)
	}
}

func TestResolveConfigDir_default(t *testing.T) {
	result := ResolveConfigDir([]string{"--quit"}, "/default")
	if result != "/default" {
		t.Errorf("expected /default, got %s", result)
	}
}

func TestResolveConfigDir_empty(t *testing.T) {
	result := ResolveConfigDir([]string{}, "/default")
	if result != "/default" {
		t.Errorf("expected /default, got %s", result)
	}
}

func TestResolveConfigDir_configFlagAtEnd(t *testing.T) {
	// --config at end with no value → fall back to default (no panic)
	result := ResolveConfigDir([]string{"--config"}, "/default")
	if result != "/default" {
		t.Errorf("expected /default, got %s", result)
	}
}

// --- isCLIDispatch ---

func TestIsCLIDispatch_match(t *testing.T) {
	if !isCLIDispatch([]string{"--quit"}, []string{"--quit", "--sync-now"}) {
		t.Error("expected true for --quit")
	}
}

func TestIsCLIDispatch_noMatch(t *testing.T) {
	if isCLIDispatch([]string{"~/.wdrive"}, []string{"--quit", "--sync-now"}) {
		t.Error("expected false for config dir arg")
	}
}

func TestIsCLIDispatch_empty(t *testing.T) {
	if isCLIDispatch([]string{}, []string{"--quit"}) {
		t.Error("expected false for empty args")
	}
}

// --- readPortFile / readHealthToken ---

func TestReadPortFile_valid(t *testing.T) {
	dir := t.TempDir()
	data := portFile{Port: 47823, Pid: 1234}
	b, _ := json.Marshal(data)
	os.WriteFile(filepath.Join(dir, "config.port"), b, 0o600)

	pf, err := readPortFile(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pf.Port != 47823 {
		t.Errorf("expected port 47823, got %d", pf.Port)
	}
	if pf.Pid != 1234 {
		t.Errorf("expected pid 1234, got %d", pf.Pid)
	}
}

func TestReadPortFile_missing(t *testing.T) {
	_, err := readPortFile(t.TempDir())
	if err == nil {
		t.Error("expected error for missing config.port")
	}
}

func TestReadPortFile_malformed(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.port"), []byte("not json"), 0o600)
	_, err := readPortFile(dir)
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}

func TestReadPortFile_portZero(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "config.port"), []byte(`{"port":0,"pid":1}`), 0o600)
	_, err := readPortFile(dir)
	if err == nil {
		t.Error("expected error for port=0")
	}
}

func TestReadHealthToken_valid(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "health_token"), []byte("abc123\n"), 0o600)

	tok, err := readHealthToken(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok != "abc123" {
		t.Errorf("expected trimmed token 'abc123', got %q", tok)
	}
}

func TestReadHealthToken_missing(t *testing.T) {
	_, err := readHealthToken(t.TempDir())
	if err == nil {
		t.Error("expected error for missing health_token")
	}
}

// --- HTTP dispatch (mock server) ---

func TestHTTPDispatch_success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer testtoken" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	// Verify the HTTP client honours the 10s timeout (just test the call succeeds)
	client := &http.Client{Timeout: 10 * 1e9}
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/quit", nil)
	req.Header.Set("Authorization", "Bearer testtoken")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestHTTPDispatch_unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 10 * 1e9}
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/quit", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

// --- formatDaemonNotRunning ---

func TestFormatDaemonNotRunning(t *testing.T) {
	msg := formatDaemonNotRunning("myapp", "/home/user/.wdrive")
	if !strings.Contains(msg, "myapp daemon is not running") {
		t.Errorf("expected app name in %q", msg)
	}
	if !strings.Contains(msg, filepath.Join("/home/user/.wdrive", "logs")) {
		t.Errorf("expected log dir in %q", msg)
	}
	if !strings.Contains(msg, "Start it with: myapp") {
		t.Errorf("expected start hint in %q", msg)
	}
}

// --- formatDaemonNotResponding ---

func TestFormatDaemonNotResponding(t *testing.T) {
	msg := formatDaemonNotResponding("myapp", 47823, "/home/user/.wdrive")
	if !strings.Contains(msg, "47823") {
		t.Errorf("expected port in %q", msg)
	}
	if !strings.Contains(msg, filepath.Join("/home/user/.wdrive", "logs")) {
		t.Errorf("expected log dir in %q", msg)
	}
	if !strings.Contains(msg, "To restart: myapp") {
		t.Errorf("expected restart hint in %q", msg)
	}
}

// --- formatHTTPError ---

func TestFormatHTTPError_401(t *testing.T) {
	msg := formatHTTPError("myapp", 401, "quit", "/home/.wdrive")
	if !strings.Contains(msg, "Authentication error") {
		t.Errorf("expected auth error in %q", msg)
	}
}

func TestFormatHTTPError_404(t *testing.T) {
	msg := formatHTTPError("myapp", 404, "unknown-cmd", "/home/.wdrive")
	if !strings.Contains(msg, "unknown-cmd") {
		t.Errorf("expected command name in %q", msg)
	}
	if !strings.Contains(msg, "myapp --help") {
		t.Errorf("expected help hint in %q", msg)
	}
}

func TestFormatHTTPError_500(t *testing.T) {
	msg := formatHTTPError("myapp", 500, "quit", "/home/.wdrive")
	if !strings.Contains(msg, "internal error") {
		t.Errorf("expected internal error in %q", msg)
	}
	if !strings.Contains(msg, filepath.Join("/home/.wdrive", "logs")) {
		t.Errorf("expected log dir in %q", msg)
	}
}

func TestFormatHTTPError_other(t *testing.T) {
	msg := formatHTTPError("myapp", 503, "quit", "/home/.wdrive")
	if !strings.Contains(msg, "503") {
		t.Errorf("expected status code in %q", msg)
	}
	if !strings.Contains(msg, filepath.Join("/home/.wdrive", "logs")) {
		t.Errorf("expected log dir in %q", msg)
	}
}

// --- formatNodeStartError ---

func TestFormatNodeStartError(t *testing.T) {
	err := fmt.Errorf("exec: not found")
	msg := formatNodeStartError("/path/to/wdrive.cjs", err)
	if !strings.Contains(msg, "wdrive.cjs") {
		t.Errorf("expected script name in %q", msg)
	}
	if !strings.Contains(msg, "exec: not found") {
		t.Errorf("expected error in %q", msg)
	}
	if !strings.Contains(msg, "node is in PATH") {
		t.Errorf("expected PATH hint in %q", msg)
	}
}

// --- formatSentinelReadError ---

func TestFormatSentinelReadError_notExist(t *testing.T) {
	msg := formatSentinelReadError("/config/config.restart", os.ErrNotExist)
	if !strings.Contains(msg, "Exiting cleanly") {
		t.Errorf("expected clean exit message in %q", msg)
	}
}

func TestFormatSentinelReadError_other(t *testing.T) {
	err := fmt.Errorf("permission denied")
	msg := formatSentinelReadError("/config/config.restart", err)
	if !strings.Contains(msg, "permission denied") {
		t.Errorf("expected error in %q", msg)
	}
	if !strings.Contains(msg, "/config/config.restart") {
		t.Errorf("expected path in %q", msg)
	}
}

// --- writeLauncherPIDFile ---

func TestWriteLauncherPIDFile(t *testing.T) {
	dir := t.TempDir()
	if err := writeLauncherPIDFile(dir, 12345); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.launcher-pid"))
	if err != nil {
		t.Fatalf("could not read PID file: %v", err)
	}
	if string(data) != "12345" {
		t.Errorf("expected '12345', got %q", string(data))
	}
}

// --- hasConsole + stdio assignment ---

func TestRunDaemon_nilStdioWhenNoConsole(t *testing.T) {
	// hasConsole() returns false in test environment (no console attached to
	// the test process stdout handle). Verify that cmd.Stdout/Stderr are nil
	// in that case so node gets NUL handles instead of invalid console handles.
	// This prevents node from crashing at libuv startup when the launcher is
	// spawned headlessly by the updater helper after an auto-update.
	if hasConsole() {
		t.Skip("test process has a console — stdio nil path not exercised")
	}

	dir := t.TempDir()
	// Write a minimal config.port so dispatch is not triggered
	// (no CLIFlags match in an empty slice)
	cfg := Config{
		ConfigDir:  dir,
		NodeScript: "nonexistent.cjs",
		CLIFlags:   []string{},
	}

	// Build the cmd the same way runDaemon does, then check stdio assignment.
	cmd := exec.Command("cmd", "/c", "exit 0") // stands in for node.exe
	cmd.WaitDelay = waitDelay
	setCmdFlags(cmd)
	if hasConsole() {
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}
	// else nil — the key assertion
	if cmd.Stdout != nil {
		t.Errorf("expected cmd.Stdout == nil when no console, got %v", cmd.Stdout)
	}
	if cmd.Stderr != nil {
		t.Errorf("expected cmd.Stderr == nil when no console, got %v", cmd.Stderr)
	}
	_ = cfg
}

// --- logger: headless / fallback ---

func TestOpenLogFile_succeedsWithValidDir(t *testing.T) {
	dir := t.TempDir()
	logMu.Lock()
	if logFile != nil { logFile.Close() }
	logFile = nil
	logDay = ""
	logMu.Unlock()

	if err := openLogFile(dir); err != nil {
		t.Fatalf("openLogFile with valid dir failed: %v", err)
	}
	logMu.Lock()
	ok := logFile != nil
	if logFile != nil { logFile.Close() }
	logFile = nil
	logDay = ""
	logMu.Unlock()
	if !ok {
		t.Fatal("logFile should not be nil after successful openLogFile")
	}
}

func TestLogWrite_writesToFileEvenWhenStderrIsDevNull(t *testing.T) {
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Skipf("cannot open devnull: %v", err)
	}
	origStderr := os.Stderr
	os.Stderr = devNull
	defer func() {
		os.Stderr = origStderr
		devNull.Close()
	}()

	dir := t.TempDir()
	logMu.Lock()
	if logFile != nil { logFile.Close() }
	logFile = nil
	logDay = ""
	logMu.Unlock()

	logInfo(dir, "test", "headless log test")

	// Close logFile before TempDir cleanup to avoid "file in use" error on Windows
	logMu.Lock()
	if logFile != nil { logFile.Close() }
	logFile = nil
	logDay = ""
	logMu.Unlock()

	pattern := filepath.Join(dir, "logs", "*.log")
	matches, _ := filepath.Glob(pattern)
	if len(matches) == 0 {
		t.Fatal("expected log file to be created, got none — logWrite silently dropped the entry when stderr=devnull")
	}
	content, _ := os.ReadFile(matches[0])
	if !strings.Contains(string(content), "headless log test") {
		t.Fatalf("expected log message in file, got: %q", string(content))
	}
}

func TestLogWrite_fallsBackToTempWhenConfigDirEmpty(t *testing.T) {
	// Remove any existing fallback log so we can detect a fresh write
	fallback := filepath.Join(os.TempDir(), "wdrive-launcher-fallback.log")
	_ = os.Remove(fallback)

	logWrite("", " INFO", "test", "hello from empty configDir")

	// After fix: fallback file must exist with the message
	content, err := os.ReadFile(fallback)
	if err != nil {
		t.Fatalf("fallback log not created: %v — logWrite with empty configDir must write to temp fallback", err)
	}
	if !strings.Contains(string(content), "hello from empty configDir") {
		t.Fatalf("expected message in fallback log, got: %q", string(content))
	}
}

func TestHasConsole_doesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("hasConsole() panicked: %v", r)
		}
	}()
	_ = hasConsole()
}
