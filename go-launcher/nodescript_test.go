package launcher

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, p string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("// bundle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestResolveNodeScriptPrefersEnvOverride(t *testing.T) {
	base := t.TempDir()
	bundle := writeFile(t, filepath.Join(base, "explicit", "app.cjs"))
	t.Setenv("LAUNCHER_BUNDLE_OVERRIDE", bundle)

	got, err := ResolveNodeScript("@scope/pkg/dist/app.cjs")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != bundle {
		t.Fatalf("got %q, want the override %q", got, bundle)
	}
}

func TestResolveNodeScriptRejectsBadEnvOverride(t *testing.T) {
	t.Setenv("LAUNCHER_BUNDLE_OVERRIDE", filepath.Join(t.TempDir(), "missing.cjs"))
	if _, err := ResolveNodeScript("app.cjs"); err == nil {
		t.Fatal("expected an error rather than a silent fallback when the override is wrong")
	}
}

func TestResolveNodeScriptAcceptsExistingPathAsGiven(t *testing.T) {
	t.Setenv("LAUNCHER_BUNDLE_OVERRIDE", "")
	bundle := writeFile(t, filepath.Join(t.TempDir(), "joined", "app.cjs"))

	got, err := ResolveNodeScript(bundle)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != bundle {
		t.Fatalf("got %q, want %q", got, bundle)
	}
}

func TestResolveNodeScriptReportsMissingBundle(t *testing.T) {
	t.Setenv("LAUNCHER_BUNDLE_OVERRIDE", "")
	_, err := ResolveNodeScript("@scope/nowhere/dist/app.cjs")
	if err == nil {
		t.Fatal("expected an error when nothing resolves")
	}
	if !filepath.IsAbs(os.Args[0]) && err == nil {
		t.Fatal("unreachable guard")
	}
}

// resolveFromNodeModules is the layout-independent part, so it is exercised directly against
// both shapes npm produces.
func TestResolveFromNodeModulesHoistedLayout(t *testing.T) {
	root := t.TempDir()
	// node_modules/@scope/pkg/dist/app.cjs  with the launcher in a sibling package
	bundle := writeFile(t, filepath.Join(root, "node_modules", "@scope", "pkg", "dist", "app.cjs"))
	launcherDir := filepath.Join(root, "node_modules", "@scope", "pkg-win32-x64")
	if err := os.MkdirAll(launcherDir, 0o755); err != nil {
		t.Fatal(err)
	}

	got := resolveFromNodeModules("@scope/pkg/dist/app.cjs", launcherDir)
	if got != bundle {
		t.Fatalf("got %q, want %q", got, bundle)
	}
}

func TestResolveFromNodeModulesNestedLayout(t *testing.T) {
	root := t.TempDir()
	bundle := writeFile(t, filepath.Join(root, "node_modules", "@scope", "pkg", "dist", "app.cjs"))
	// npm nested the platform package under the main one instead of hoisting it.
	launcherDir := filepath.Join(root, "node_modules", "@scope", "pkg", "node_modules", "@scope", "pkg-win32-x64")
	if err := os.MkdirAll(launcherDir, 0o755); err != nil {
		t.Fatal(err)
	}

	got := resolveFromNodeModules("@scope/pkg/dist/app.cjs", launcherDir)
	if got != bundle {
		t.Fatalf("got %q, want %q", got, bundle)
	}
}

func TestResolveFromNodeModulesReturnsEmptyWhenAbsent(t *testing.T) {
	if got := resolveFromNodeModules("@scope/pkg/dist/app.cjs", t.TempDir()); got != "" {
		t.Fatalf("got %q, want an empty string", got)
	}
}
