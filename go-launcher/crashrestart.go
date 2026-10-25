package launcher

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync/atomic"
	"time"
)

// CrashRestart is an opt-in, bounded restart of a daemon that exits on its own with a failure.
//
// Without it (Config.CrashRestart == nil) a crash ends the launcher: it exits with the daemon's code and nothing starts
// the daemon again. That is the right default for a consumer whose daemon is cheap to start by hand, and the wrong one
// for a scheduler: orchestrator stayed dead for 47 minutes on 2026-10-25 after one uncaught exception.
//
// With it, a non-zero exit that nobody asked for is followed by a restart after a back-off, at most MaxRestarts times
// in any Window. The next crash after that is not restarted: the launcher logs why at ERROR level, with the log
// directory and the command to start again, and exits with the daemon's last code.
//
// Never a crash: an exit code 0 (a stop, or a restart/update requested through a sentinel file), and an exit while the
// launcher itself had been asked to stop by a signal it forwarded.
type CrashRestart struct {
	// Delays[n] is the wait before the n+1th restart within the Window; the last one is reused. Default 2s, 10s, 30s.
	Delays []time.Duration
	// MaxRestarts is the number of crash restarts allowed in any Window. Default 3.
	MaxRestarts int
	// Window is the period MaxRestarts is counted over. Default 10 minutes.
	Window time.Duration
	// StartCommand is what the log tells the user to run once the launcher has given up. Default: the app name.
	StartCommand string
}

// CrashMarkerFile is written in the config dir when the daemon exits with a failure and left no report of its own.
// A daemon that writes the same file itself (with the cause) is never overwritten.
const CrashMarkerFile = "crashed.json"

func (c *CrashRestart) withDefaults(app string) CrashRestart {
	p := CrashRestart{}
	if c != nil {
		p = *c
	}
	if len(p.Delays) == 0 {
		p.Delays = []time.Duration{2 * time.Second, 10 * time.Second, 30 * time.Second}
	}
	if p.MaxRestarts <= 0 {
		p.MaxRestarts = 3
	}
	if p.Window <= 0 {
		p.Window = 10 * time.Minute
	}
	if p.StartCommand == "" {
		p.StartCommand = app
	}
	return p
}

// daemonExit is how one run of the daemon ended.
type daemonExit struct {
	code int
	// signalled: the launcher had forwarded a stop signal to the daemon, so this exit was asked for.
	signalled bool
	pid       int
}

// signalReceived is set by forwardSignals when a stop signal reached the launcher; runDaemonOnce resets it per run.
var signalReceived atomic.Bool

// loopDeps are the seams of superviseDaemon: the real ones start a process and sleep; the tests' do neither.
type loopDeps struct {
	runOnce   func(cfg Config, args []string) daemonExit
	sentinels func(configDir string, cfg Config) sentinelResult
	update    func(configDir string, cmd []string)
	// sleep waits for the delay and reports whether a stop signal of the launcher cut it short.
	sleep  func(time.Duration) bool
	now    func() time.Time
	info   func(msg string)
	warn   func(msg string)
	errorf func(msg string)
}

func realLoopDeps(cfg Config) loopDeps {
	return loopDeps{
		runOnce:   runDaemonOnce,
		sentinels: checkSentinels,
		update:    spawnUpdateAndExit,
		sleep:     sleepOrSignal,
		now:       time.Now,
		info:      func(msg string) { logInfo(cfg.ConfigDir, "launcher", msg) },
		warn:      func(msg string) { logWarn(cfg.ConfigDir, "launcher", msg) },
		errorf:    func(msg string) { logError(cfg.ConfigDir, "launcher", msg) },
	}
}

// superviseDaemon runs the daemon until it ends for good and returns the code the launcher exits with.
func superviseDaemon(cfg Config, args []string, d loopDeps) int {
	policy := cfg.CrashRestart.withDefaults(appName(cfg))
	var crashes []time.Time
	for {
		runStart := d.now()
		res := d.runOnce(cfg, args)

		// Check sentinels written by Node before it exited.
		// Node cannot spawn a new launcher binary outside the Job Object — only the Go
		// launcher (which is not in the Job Object) can safely re-spawn or trigger an update.
		d.info(fmt.Sprintf("post-exit check: exitCode=%d", res.code))
		if res.code == 0 {
			result := d.sentinels(cfg.ConfigDir, cfg)
			switch result.action {
			case sentinelUpdate:
				d.update(cfg.ConfigDir, cfg.UpdateCmd)
				return 0
			case sentinelRestart:
				continue
			case sentinelNone:
				// No sentinel: the daemon stopped on purpose.
			}
			return 0
		}

		if cfg.CrashRestart == nil || res.signalled {
			return res.code
		}

		now := d.now()
		kept := crashes[:0]
		for _, t := range crashes {
			if now.Sub(t) < policy.Window {
				kept = append(kept, t)
			}
		}
		crashes = append(kept, now)
		n := len(crashes)

		d.errorf(fmt.Sprintf("daemon crashed: %s (crash %d in the last %s)", describeExit(res.code), n, describeWindow(policy.Window)))
		writeCrashMarker(cfg.ConfigDir, res, runStart, now, d.warn)

		if n > policy.MaxRestarts {
			d.errorf(fmt.Sprintf(
				"daemon crashed %d times in %s, not restarting: see %s, then run %s",
				n, describeWindow(policy.Window), filepath.Join(cfg.ConfigDir, "logs"), policy.StartCommand,
			))
			return res.code
		}
		delay := policy.Delays[min(n-1, len(policy.Delays)-1)]
		d.warn(fmt.Sprintf("restarting in %s (crash %d of %d allowed in %s)", delay, n, policy.MaxRestarts, describeWindow(policy.Window)))
		if d.sleep(delay) {
			d.warn(fmt.Sprintf("stop signal received while waiting to restart: not restarting, exiting with the daemon's last code %d", res.code))
			return res.code
		}
	}
}

// sleepOrSignal waits for d and returns false, or returns true as soon as the launcher itself is asked to stop (Ctrl+C,
// and SIGTERM on POSIX). Between two runs no daemon is there to forward a signal to, and without a handler the signal would
// kill the launcher without a word in its log.
func sleepOrSignal(d time.Duration) bool {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, stopSignals...)
	defer signal.Stop(ch)
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ch:
		return true
	case <-timer.C:
		return false
	}
}

// describeExit says what an exit code means for the log and the marker. -1 is what Go reports for a process that did not
// exit by itself: POSIX death by a signal (SIGKILL, the OOM killer's). On Windows a forced kill (taskkill /F) shows as 1.
func describeExit(code int) string {
	if code < 0 {
		return fmt.Sprintf("node exited with code %d (killed by a signal or forcibly terminated, e.g. SIGKILL or the OOM killer)", code)
	}
	return fmt.Sprintf("node exited with code %d", code)
}

// describeWindow renders a duration for the log: "10 minutes", "1 minute", or Go's own form below a minute.
func describeWindow(w time.Duration) string {
	if w >= time.Minute && w%time.Minute == 0 {
		if w == time.Minute {
			return "1 minute"
		}
		return fmt.Sprintf("%d minutes", int(w/time.Minute))
	}
	return w.String()
}

// markerSourceLauncher is the "source" value of a marker written by the launcher. A marker without it is the daemon's own.
const markerSourceLauncher = "launcher"

// writeCrashMarker leaves <configDir>/crashed.json for a status command. Never fails the launcher: a marker that cannot be
// written is a warning.
//
// Which report wins:
//   - no file: the launcher's is written (O_EXCL, so a daemon writing at the same instant is not overwritten);
//   - a file the launcher wrote ("source":"launcher") dated before runStart, the start of the run that just crashed: it
//     reports an earlier crash that nobody cleared, so it is replaced by this crash;
//   - anything else is kept: the daemon's own report (it says why, whenever it was written), a launcher report not older
//     than the run, a file that is not valid JSON (warned, with the file to delete).
func writeCrashMarker(configDir string, res daemonExit, runStart, now time.Time, warn func(string)) {
	path := filepath.Join(configDir, CrashMarkerFile)
	body, err := json.MarshalIndent(map[string]any{
		"time":    now.UTC().Format(time.RFC3339Nano),
		"kind":    "exit",
		"source":  markerSourceLauncher,
		"error":   describeExit(res.code) + " and left no crash report of its own",
		"version": "unknown",
		"pid":     res.pid,
	}, "", "  ")
	if err != nil {
		warn(fmt.Sprintf("could not build the crash marker: %v", err))
		return
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err == nil {
		defer f.Close()
		if _, err := f.Write(body); err != nil {
			warn(fmt.Sprintf("could not write the crash marker %s: %v", path, err))
		}
		return
	}
	if !os.IsExist(err) {
		warn(fmt.Sprintf("could not write the crash marker %s: %v (status commands will not report this crash)", path, err))
		return
	}

	existing, err := os.ReadFile(path)
	if err != nil {
		warn(fmt.Sprintf("could not read the existing crash marker %s: %v (status commands may report an older crash)", path, err))
		return
	}
	var prev struct {
		Time   string `json:"time"`
		Source string `json:"source"`
	}
	if err := json.Unmarshal(existing, &prev); err != nil {
		warn(fmt.Sprintf("the existing crash marker %s is not valid JSON (%v), so the launcher left it: delete it to get this crash reported", path, err))
		return
	}
	if prev.Source != markerSourceLauncher {
		return
	}
	prevTime, err := time.Parse(time.RFC3339Nano, prev.Time)
	// An unparseable time on the launcher's own file is treated as stale; a time not before the run is not about an earlier run.
	if err == nil && !prevTime.Before(runStart) {
		return
	}
	// Written beside the marker, then renamed over it: a status command never reads half a file.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		warn(fmt.Sprintf("could not write the crash marker %s: %v (status commands will keep reporting the previous crash)", tmp, err))
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		warn(fmt.Sprintf("could not replace the crash marker %s: %v (status commands will keep reporting the previous crash)", path, err))
	}
}
