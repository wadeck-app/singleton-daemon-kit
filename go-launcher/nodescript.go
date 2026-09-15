package launcher

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// launcherDir returns the directory holding the running launcher binary, with symlinks
// resolved so it points at the real location rather than the symlink's directory.
func launcherDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, rErr := filepath.EvalSymlinks(exe); rErr == nil {
		exe = resolved
	}
	return filepath.Dir(exe), nil
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// resolveFromNodeModules probes <dir>/node_modules/<spec> walking up from startDir, the way
// Node resolves a package specifier. Returns "" when nothing matches.
//
// This is what lets a launcher shipped in its own platform package find a bundle that lives
// in a sibling package: npm may hoist the platform package next to the main one, or nest it
// under <main>/node_modules/, and both layouts are reached by walking up.
func resolveFromNodeModules(spec, startDir string) string {
	rel := filepath.FromSlash(spec)
	dir := startDir
	for {
		if candidate := filepath.Join(dir, "node_modules", rel); isFile(candidate) {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// ResolveNodeScript locates the .cjs bundle to spawn, in this order:
//
//  1. LAUNCHER_BUNDLE_OVERRIDE, set by the npm shim when it already knows the absolute path.
//  2. spec as given, when it is an existing file. Covers an absolute path, and consumers on
//     an older main.go.tmpl that joined it to the exe directory before calling Run.
//  3. spec relative to the launcher's own directory, for the layout where the bundle ships
//     next to the binary.
//  4. spec as an npm package specifier, resolved by walking up node_modules.
//
// Step 4 exists because a bare launch (a Windows HKCU\Run value, a launchd plist) cannot
// inject LAUNCHER_BUNDLE_OVERRIDE, and a fixed relative path between two npm packages is not
// a property anyone can rely on: npm decides whether to hoist or nest.
func ResolveNodeScript(spec string) (string, error) {
	if override := strings.TrimSpace(os.Getenv("LAUNCHER_BUNDLE_OVERRIDE")); override != "" {
		if isFile(override) {
			return override, nil
		}
		return "", fmt.Errorf("LAUNCHER_BUNDLE_OVERRIDE points at %q, which is not a file", override)
	}

	if spec == "" {
		return "", fmt.Errorf("no bundle configured: NodeScript is empty")
	}

	if isFile(spec) {
		return spec, nil
	}

	exeDir, err := launcherDir()
	if err != nil {
		return "", fmt.Errorf("cannot locate the launcher binary: %w", err)
	}

	if nextTo := filepath.Join(exeDir, filepath.FromSlash(spec)); isFile(nextTo) {
		return nextTo, nil
	}

	if fromPkg := resolveFromNodeModules(spec, exeDir); fromPkg != "" {
		return fromPkg, nil
	}

	return "", fmt.Errorf(
		"cannot find the bundle %q: not next to the launcher (%s), and no node_modules/%s "+
			"found walking up from there",
		spec, exeDir, filepath.ToSlash(spec))
}
