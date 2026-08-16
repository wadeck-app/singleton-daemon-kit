// Package launcher provides a named .exe wrapper for Node.js CLI daemons.
// It replaces shell scripts (.cmd/.sh) with a proper named binary visible
// in Task Manager, while keeping Node.js as the actual daemon runtime.
//
// Usage: call Run() from main() with your project-specific Config.
// The binary automatically selects CLI dispatch or daemon spawn mode
// based on os.Args.
package launcher

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// waitDelay is the maximum time to wait for I/O pipes to close after the child
// process exits. Required when grandchildren (e.g. tray binary) inherit pipe
// handles — without this, cmd.Wait() blocks until all grandchildren exit.
// Value: 5s covers any normal tray/watcher shutdown; returns exec.ErrWaitDelay if exceeded.
const waitDelay = 5 * time.Second

// Config holds all project-specific settings for the launcher.
type Config struct {
	// ConfigDir is the path to the daemon's config directory
	// (where config.port and health_token live, e.g. ~/.wdrive).
	ConfigDir string

	// NodeScript is the absolute path to the .cjs bundle to spawn in daemon mode.
	NodeScript string

	// CLIFlags lists the flags that trigger HTTP dispatch instead of daemon spawn.
	// Example: []string{"--quit", "--sync-now", "--pause", "--resume", "--restart"}
	CLIFlags []string

	// DefaultPort is used for error messages when config.port is absent. Defaults to 47823.
	DefaultPort int

	// AppName is the binary name used in user-facing error messages (e.g. "wdrive").
	// Defaults to the executable basename when empty.
	AppName string

	// SilentFlags lists flags for which the launcher suppresses its own INFO/WARN
	// log output on stderr. Error logs are always shown. File logging is unaffected.
	// Use for short-lived pass-through commands (--help, --version, --pid) where
	// launcher lifecycle noise would obscure the actual command output.
	SilentFlags []string
}

// Run is the launcher entrypoint — it never returns.
// It inspects os.Args and either dispatches a CLI command via HTTP
// or spawns the Node.js daemon process.
// --help, --version and any other flags not in CLIFlags are passed through to Node.
func appName(cfg Config) string {
	if cfg.AppName != "" {
		return cfg.AppName
	}
	if exe, err := os.Executable(); err == nil {
		return strings.TrimSuffix(filepath.Base(exe), ".exe")
	}
	return "app"
}

func Run(cfg Config) {
	if cfg.DefaultPort == 0 {
		cfg.DefaultPort = 47823
	}

	args := os.Args[1:]

	// Suppress launcher INFO/WARN stderr output for short-lived pass-through commands
	// (e.g. --help, --version, --pid) so their output is not buried in lifecycle noise.
	for _, arg := range args {
		for _, silent := range cfg.SilentFlags {
			if arg == silent {
				setSilentMode(true)
				break
			}
		}
	}
	if isCLIDispatch(args, cfg.CLIFlags) {
		runCLIDispatch(cfg, args)
	} else {
		runDaemon(cfg, args)
	}
}

// isCLIDispatch returns true when any argument matches a known CLI flag.
func isCLIDispatch(args []string, cliFlags []string) bool {
	for _, arg := range args {
		for _, flag := range cliFlags {
			if arg == flag {
				return true
			}
		}
	}
	return false
}

// runCLIDispatch sends a command to the running daemon via HTTP and exits.
// Node.js is never launched.
func runCLIDispatch(cfg Config, args []string) {
	pf, err := readPortFile(cfg.ConfigDir)
	if err != nil {
		logError(cfg.ConfigDir, "dispatch", err.Error())
		fmt.Fprintln(os.Stderr, formatDaemonNotRunning(appName(cfg), cfg.ConfigDir))
		os.Exit(1)
	}

	token, err := readHealthToken(cfg.ConfigDir)
	if err != nil {
		logError(cfg.ConfigDir, "dispatch", err.Error())
		fmt.Fprintln(os.Stderr, formatDaemonNotRunning(appName(cfg), cfg.ConfigDir))
		os.Exit(1)
	}

	// Extract command name: first --flag that is in cfg.CLIFlags (skip --config <value> pairs)
	command := ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--config" {
			i++ // skip the value
			continue
		}
		if strings.HasPrefix(arg, "--") {
			for _, flag := range cfg.CLIFlags {
				if arg == flag {
					command = strings.TrimPrefix(arg, "--")
					break
				}
			}
			if command != "" {
				break
			}
		}
	}
	if command == "" {
		fmt.Fprintln(os.Stderr, "Error: no recognised command flag found in arguments")
		os.Exit(1)
	}

	url := fmt.Sprintf("http://127.0.0.1:%d/%s", pf.Port, command)
	logInfo(cfg.ConfigDir, "dispatch", fmt.Sprintf("POST %s (pid %d)", url, pf.Pid))

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader([]byte("{}")))
	if err != nil {
		logError(cfg.ConfigDir, "dispatch", fmt.Sprintf("build request: %v", err))
		os.Exit(1)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		logError(cfg.ConfigDir, "dispatch", fmt.Sprintf("HTTP error: %v", err))
		fmt.Fprintln(os.Stderr, formatDaemonNotResponding(appName(cfg), pf.Port, cfg.ConfigDir))
		os.Exit(1)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		logError(cfg.ConfigDir, "dispatch", fmt.Sprintf("HTTP %d: %s", resp.StatusCode, body))
		fmt.Fprintln(os.Stderr, formatHTTPError(appName(cfg), resp.StatusCode, command, cfg.ConfigDir))
		os.Exit(1)
	}

	logInfo(cfg.ConfigDir, "dispatch", fmt.Sprintf("command '%s' OK", command))
	fmt.Println(strings.TrimSpace(string(body)))
	os.Exit(0)
}

// runDaemon spawns node.exe with the .cjs script and stays alive until it exits.
// On Windows a Job Object ensures the child is killed when this process dies.
func runDaemon(cfg Config, args []string) {
	nodePath, err := exec.LookPath("node")
	if err != nil {
		logError(cfg.ConfigDir, "launcher", "node not found in PATH — install Node.js and ensure it is in PATH")
		os.Exit(1)
	}

	scriptArgs := append([]string{cfg.NodeScript}, args...)
	cmd := exec.Command(nodePath, scriptArgs...)
	// Pass os.Stdout/Stderr as *os.File — Go hands the raw handles to CreateProcess,
	// no internal I/O goroutines are created. cmd.Wait() returns as soon as node exits.
	// WaitDelay is a safety net: if grandchildren (tray) still hold copies of the handle,
	// cmd.Wait() force-closes after 5s rather than blocking indefinitely.
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.WaitDelay = waitDelay
	setCmdFlags(cmd)

	logInfo(cfg.ConfigDir, "launcher", fmt.Sprintf("configDir=%s", cfg.ConfigDir))
	logInfo(cfg.ConfigDir, "launcher", fmt.Sprintf("spawning %s %s", nodePath, strings.Join(scriptArgs, " ")))

	if err := cmd.Start(); err != nil {
		logError(cfg.ConfigDir, "launcher", formatNodeStartError(cfg.NodeScript, err))
		os.Exit(1)
	}

	launcherPID := os.Getpid()
	if pidErr := writeLauncherPIDFile(cfg.ConfigDir, launcherPID); pidErr != nil {
		logWarn(cfg.ConfigDir, "launcher", fmt.Sprintf("could not write launcher PID file: %v", pidErr))
	}
	logInfo(cfg.ConfigDir, "launcher", fmt.Sprintf("PIDs: launcher=%d node=%d", launcherPID, cmd.Process.Pid))

	if err := assignJobObject(cmd); err != nil {
		logWarn(cfg.ConfigDir, "launcher", fmt.Sprintf("Job Object setup failed (non-fatal): %v", err))
	}

	stopSignals := forwardSignals(cmd)

	// cmd.Wait() with WaitDelay=5s:
	// - os.Stdout/Stderr are *os.File so no I/O goroutines are created.
	// - cmd.Wait() returns as soon as node exits in the normal case.
	// - If grandchildren (tray binary) still hold handle copies, WaitDelay
	//   force-closes after 5s and returns exec.ErrWaitDelay — still treated as
	//   a clean exit since node itself has already exited.
	exitCode := 0
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
			logInfo(cfg.ConfigDir, "launcher", fmt.Sprintf("node exited with code %d", exitCode))
		} else if err == exec.ErrWaitDelay {
			logInfo(cfg.ConfigDir, "launcher", "node exited (WaitDelay triggered for grandchildren)")
		} else {
			logError(cfg.ConfigDir, "launcher", fmt.Sprintf("node wait error: %v", err))
			exitCode = 1
		}
	} else {
		logInfo(cfg.ConfigDir, "launcher", "node exited cleanly")
	}

	close(stopSignals)

	// Check for restart sentinel written by Node before it exited.
	// Node cannot spawn a new wdrive.exe outside the Job Object — only the Go
	// launcher (which is not in the Job Object) can safely re-spawn.
	sentinelPath := filepath.Join(cfg.ConfigDir, "config.restart")
	logInfo(cfg.ConfigDir, "launcher", fmt.Sprintf("post-exit check: exitCode=%d sentinelPath=%s", exitCode, sentinelPath))
	if exitCode == 0 {
		if _, err := os.Stat(sentinelPath); err == nil {
			logInfo(cfg.ConfigDir, "launcher", "restart sentinel detected — relaunching")
			if removeErr := os.Remove(sentinelPath); removeErr == nil {
				runDaemon(cfg, args)
				return
			} else {
				logWarn(cfg.ConfigDir, "launcher", fmt.Sprintf("restart sentinel found but could not remove (%v) — not relaunching", removeErr))
			}
		} else {
			sentinelMsg := formatSentinelReadError(sentinelPath, err)
			if os.IsNotExist(err) {
				logInfo(cfg.ConfigDir, "launcher", sentinelMsg)
			} else {
				logWarn(cfg.ConfigDir, "launcher", sentinelMsg)
			}
		}
	}

	os.Exit(exitCode)
}

// ResolveConfigDir extracts --config <dir> from args, or returns defaultDir.
func ResolveConfigDir(args []string, defaultDir string) string {
	for i, arg := range args {
		if arg == "--config" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return defaultDir
}

// DefaultConfigDir returns the platform-appropriate default config directory (~/.appName).
func DefaultConfigDir(appName string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "." + string(filepath.Separator) + "." + appName
	}
	return filepath.Join(home, "."+appName)
}
