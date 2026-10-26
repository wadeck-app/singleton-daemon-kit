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

// Linux has a launcher like the other platforms: without it a consumer's npm install on Linux has nothing to start the
// daemon with. The five binaries are all built by one call, so a consumer cannot get a subset.
func TestBuildSh_buildsTheLauncherForEveryPlatformIncludingLinux(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatalf("bash is required on PATH to test build.sh: %v", err)
	}
	dir := t.TempDir()
	cfg := filepath.Join(dir, "launcher.config.json")
	body := `{"appName":"x","nodeScript":"x.cjs","defaultConfigDir":"x","cliFlags":[]}`
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")
	if combined, err := exec.Command(bash, "build.sh", filepath.ToSlash(cfg), filepath.ToSlash(out)).CombinedOutput(); err != nil {
		t.Fatalf("build.sh failed: %v\n%s", err, combined)
	}
	for _, name := range []string{
		"x_windows_release.exe", "x_darwin_arm64_release", "x_darwin_amd64_release",
		"x_linux_amd64_release", "x_linux_arm64_release",
	} {
		if _, err := os.Stat(filepath.Join(out, name)); err != nil {
			t.Errorf("build.sh did not produce %s: %v", name, err)
		}
	}
}
