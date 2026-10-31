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

// pathWithout returns PATH minus the directory holding the named tool, so a build runs as on a machine that lacks it.
// It fails loudly if that directory also holds bash, go or node: removing it would break the build for another reason.
func pathWithout(t *testing.T, tool string) string {
	t.Helper()
	found, err := exec.LookPath(tool)
	if err != nil {
		t.Skipf("%s is not installed here, so PATH already lacks it", tool)
	}
	skip := filepath.Dir(found)
	var kept []string
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if strings.EqualFold(filepath.Clean(p), skip) {
			continue
		}
		kept = append(kept, p)
	}
	for _, needed := range []string{"bash", "go", "node"} {
		saved := os.Getenv("PATH")
		t.Setenv("PATH", strings.Join(kept, string(os.PathListSeparator)))
		_, err := exec.LookPath(needed)
		t.Setenv("PATH", saved)
		if err != nil {
			t.Fatalf("%s lives in the same directory as %s (%s): cannot build a PATH without one and keep the other: %v", needed, tool, skip, err)
		}
	}
	return strings.Join(kept, string(os.PathListSeparator))
}

// runBuildShWithoutGoversioninfo runs build.sh with a PATH lacking goversioninfo and the given extra environment.
func runBuildShWithoutGoversioninfo(t *testing.T, extraEnv ...string) (string, error) {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatalf("bash is required on PATH to test build.sh: %v", err)
	}
	path := pathWithout(t, "goversioninfo")
	dir := t.TempDir()
	cfg := filepath.Join(dir, "launcher.config.json")
	body := `{"appName":"x","nodeScript":"x.cjs","defaultConfigDir":"x","cliFlags":[]}`
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bash, "build.sh", filepath.ToSlash(cfg), filepath.ToSlash(filepath.Join(dir, "out")))
	// Neutral defaults first: a CI runner must not leak its own GITHUB_ACTIONS / strict setting into the test.
	cmd.Env = append(os.Environ(), "PATH="+path, "GITHUB_ACTIONS=", "LAUNCHER_REQUIRE_PE_METADATA=")
	cmd.Env = append(cmd.Env, extraEnv...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// Without goversioninfo the published Windows launcher silently lacks FileDescription / ProductName: the build says so
// loudly, with the install command, but still succeeds so local builds keep working.
func TestBuildSh_aMissingGoversioninfoWarnsWithTheInstallCommandAndStillBuilds(t *testing.T) {
	out, err := runBuildShWithoutGoversioninfo(t)
	if err != nil {
		t.Fatalf("a missing goversioninfo must only warn, but build.sh failed: %v\n%s", err, out)
	}
	for _, want := range []string{
		"WARNING", "goversioninfo", "FileDescription", "ProductName",
		"go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest", "LAUNCHER_REQUIRE_PE_METADATA=1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the warning lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "::warning ") {
		t.Errorf("a GitHub Actions annotation was emitted outside GitHub Actions:\n%s", out)
	}
}

func TestBuildSh_aMissingGoversioninfoEmitsAGitHubAnnotationInGitHubActions(t *testing.T) {
	out, err := runBuildShWithoutGoversioninfo(t, "GITHUB_ACTIONS=true")
	if err != nil {
		t.Fatalf("a missing goversioninfo must only warn, but build.sh failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "::warning ") || !strings.Contains(out, "goversioninfo/cmd/goversioninfo@latest") {
		t.Errorf("no ::warning annotation carrying the install command:\n%s", out)
	}
}

// Opt-in strict mode: a release pipeline sets it so a launcher without metadata is never published.
func TestBuildSh_strictModeFailsWhenGoversioninfoIsMissing(t *testing.T) {
	out, err := runBuildShWithoutGoversioninfo(t, "LAUNCHER_REQUIRE_PE_METADATA=1")
	if err == nil {
		t.Fatalf("LAUNCHER_REQUIRE_PE_METADATA=1 must fail the build when goversioninfo is missing:\n%s", out)
	}
	for _, want := range []string{"goversioninfo", "go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest", "LAUNCHER_REQUIRE_PE_METADATA"} {
		if !strings.Contains(out, want) {
			t.Errorf("the strict-mode error lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Building 5 targets") {
		t.Errorf("strict mode must fail before compiling anything:\n%s", out)
	}
}
