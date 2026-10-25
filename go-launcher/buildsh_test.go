package launcher

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// CrashRestart is Go-only (Config.CrashRestart is json:"-"). A launcher.config.json that carries a "crashRestart" key
// would be silently ignored by build.sh, and the consumer would ship a launcher that never restarts: build.sh must stop
// and say what to do instead.
func TestBuildSh_aCrashRestartKeyInLauncherConfigIsAnExplicitError(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatalf("bash is required on PATH to test build.sh: %v", err)
	}
	dir := t.TempDir()
	cfg := filepath.Join(dir, "launcher.config.json")
	body := `{"appName":"x","nodeScript":"x.cjs","defaultConfigDir":"x","cliFlags":[],"crashRestart":{"maxRestarts":5}}`
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// Forward slashes: the script requires the path through node's require() under Git Bash on Windows.
	out, err := exec.Command(bash, "build.sh", filepath.ToSlash(cfg), filepath.ToSlash(filepath.Join(dir, "out"))).CombinedOutput()
	if err == nil {
		t.Fatalf("build.sh accepted a crashRestart key and would ignore it silently:\n%s", out)
	}
	for _, want := range []string{"crashRestart", "launcher.CrashRestart{", "main.go.tmpl"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("the error lacks %q:\n%s", want, out)
		}
	}
}
