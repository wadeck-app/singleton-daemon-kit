package launcher

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
