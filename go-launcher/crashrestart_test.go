package launcher

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// What the launcher does when the daemon it started exits on its own with a failure.
//
// Without a policy (Config.CrashRestart == nil) a crash is the end: the launcher exits with the daemon's code and nothing
// restarts it. That left a scheduler dead for 47 minutes after one uncaught exception (2026-10-25). With a policy the
// launcher restarts the daemon after a back-off, at most MaxRestarts times in any Window, and then stops saying so.
// The runner, the clock and the sleep are injected: no process is started and no test waits.

type fakeDaemon struct {
	t        *testing.T
	exits    []daemonExit // consumed in order; the loop must not ask for more than given
	runs     int
	sleeps   []time.Duration
	clock    time.Time
	runTime  time.Duration // how long each run "lasted"
	infos    []string
	warns    []string
	errors   []string
	updates  [][]string
	sentinel []sentinelResult // consumed in order, one per exit 0
	// interruptSleep: the nth sleep (1-based) is cut short by a stop signal of the launcher; 0 = never.
	interruptSleep int
	// duringRun runs inside the nth run (0-based), where the daemon would write its own files.
	duringRun func(run int, now time.Time)
}

func newFakeDaemon(t *testing.T, exits ...daemonExit) *fakeDaemon {
	return &fakeDaemon{t: t, exits: exits, clock: time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC), runTime: time.Minute}
}

func (f *fakeDaemon) deps() loopDeps {
	return loopDeps{
		runOnce: func(cfg Config, args []string) daemonExit {
			if f.runs >= len(f.exits) {
				f.t.Fatalf("the loop started the daemon a %dth time, only %d runs were planned: it restarts without bound", f.runs+1, len(f.exits))
			}
			e := f.exits[f.runs]
			f.runs++
			f.clock = f.clock.Add(f.runTime)
			if f.duringRun != nil {
				f.duringRun(f.runs-1, f.clock)
			}
			return e
		},
		sentinels: func(string, Config) sentinelResult {
			if len(f.sentinel) == 0 {
				return sentinelResult{action: sentinelNone}
			}
			s := f.sentinel[0]
			f.sentinel = f.sentinel[1:]
			return s
		},
		update: func(_ string, cmd []string) { f.updates = append(f.updates, cmd) },
		sleep: func(d time.Duration) bool {
			f.sleeps = append(f.sleeps, d)
			f.clock = f.clock.Add(d)
			return f.interruptSleep != 0 && len(f.sleeps) == f.interruptSleep
		},
		now:    func() time.Time { return f.clock },
		info:   func(msg string) { f.infos = append(f.infos, msg) },
		warn:   func(msg string) { f.warns = append(f.warns, msg) },
		errorf: func(msg string) { f.errors = append(f.errors, msg) },
	}
}

func (f *fakeDaemon) allLogs() string {
	return strings.Join(append(append(append([]string{}, f.infos...), f.warns...), f.errors...), "\n")
}

func crash(code int) daemonExit { return daemonExit{code: code, pid: 4242} }
func clean() daemonExit         { return daemonExit{code: 0, pid: 4242} }
func policy() *CrashRestart     { return &CrashRestart{StartCommand: "orch start"} }
func cfgIn(t *testing.T) Config {
	return Config{ConfigDir: t.TempDir(), AppName: "orchestrator", CrashRestart: policy()}
}

func TestCrashRestart_defaults(t *testing.T) {
	p := (&CrashRestart{}).withDefaults("orchestrator")
	if len(p.Delays) != 3 || p.Delays[0] != 2*time.Second || p.Delays[1] != 10*time.Second || p.Delays[2] != 30*time.Second {
		t.Errorf("default delays should be 2s,10s,30s, got %v", p.Delays)
	}
	if p.MaxRestarts != 3 {
		t.Errorf("default MaxRestarts should be 3, got %d", p.MaxRestarts)
	}
	if p.Window != 10*time.Minute {
		t.Errorf("default Window should be 10 minutes, got %v", p.Window)
	}
	if p.StartCommand != "orchestrator" {
		t.Errorf("StartCommand should default to the app name, got %q", p.StartCommand)
	}
}

func TestSupervise_cleanExitEndsTheLauncher(t *testing.T) {
	f := newFakeDaemon(t, clean())
	if code := superviseDaemon(cfgIn(t), nil, f.deps()); code != 0 {
		t.Errorf("a clean exit must end the launcher with 0, got %d", code)
	}
	if f.runs != 1 || len(f.sleeps) != 0 {
		t.Errorf("runs=%d sleeps=%v, want one run and no sleep", f.runs, f.sleeps)
	}
}

func TestSupervise_withoutAPolicyACrashIsTheEnd(t *testing.T) {
	f := newFakeDaemon(t, crash(1))
	cfg := cfgIn(t)
	cfg.CrashRestart = nil
	if code := superviseDaemon(cfg, nil, f.deps()); code != 1 {
		t.Errorf("without a policy the launcher exits with the daemon's code, got %d", code)
	}
	if f.runs != 1 {
		t.Errorf("a daemon of a consumer without a policy was restarted (%d runs)", f.runs)
	}
}

func TestSupervise_restartSentinelRelaunchesWithoutDelayAndIsNotACrash(t *testing.T) {
	f := newFakeDaemon(t, clean(), clean())
	f.sentinel = []sentinelResult{{action: sentinelRestart}}
	if code := superviseDaemon(cfgIn(t), nil, f.deps()); code != 0 {
		t.Errorf("got %d", code)
	}
	if f.runs != 2 || len(f.sleeps) != 0 {
		t.Errorf("runs=%d sleeps=%v, want two runs and no sleep", f.runs, f.sleeps)
	}
	if strings.Contains(f.allLogs(), "crashed") {
		t.Errorf("a requested restart was reported as a crash:\n%s", f.allLogs())
	}
}

func TestSupervise_updateSentinelRunsTheUpdateAndEnds(t *testing.T) {
	f := newFakeDaemon(t, clean())
	f.sentinel = []sentinelResult{{action: sentinelUpdate}}
	cfg := cfgIn(t)
	cfg.UpdateCmd = []string{"npm", "install", "-g", "@wadeck-app/x"}
	if code := superviseDaemon(cfg, nil, f.deps()); code != 0 {
		t.Errorf("got %d", code)
	}
	if len(f.updates) != 1 || f.runs != 1 {
		t.Errorf("updates=%v runs=%d", f.updates, f.runs)
	}
}

func TestSupervise_aCrashIsRestartedWithBackoff(t *testing.T) {
	f := newFakeDaemon(t, crash(1), crash(1), crash(1), clean())
	if code := superviseDaemon(cfgIn(t), nil, f.deps()); code != 0 {
		t.Errorf("a daemon that ends cleanly after crashes ends the launcher with 0, got %d", code)
	}
	want := []time.Duration{2 * time.Second, 10 * time.Second, 30 * time.Second}
	if len(f.sleeps) != 3 || f.sleeps[0] != want[0] || f.sleeps[1] != want[1] || f.sleeps[2] != want[2] {
		t.Errorf("sleeps = %v, want %v", f.sleeps, want)
	}
	if f.runs != 4 {
		t.Errorf("runs = %d, want 4", f.runs)
	}
}

func TestSupervise_theFourthCrashInTenMinutesIsNotRestarted(t *testing.T) {
	f := newFakeDaemon(t, crash(1), crash(1), crash(1), crash(7))
	f.runTime = 10 * time.Second
	cfg := cfgIn(t)
	if code := superviseDaemon(cfg, nil, f.deps()); code != 7 {
		t.Errorf("when the launcher gives up it exits with the daemon's last code (7), got %d", code)
	}
	if f.runs != 4 {
		t.Errorf("runs = %d, want 4 (the first start and 3 restarts)", f.runs)
	}
	if len(f.sleeps) != 3 {
		t.Errorf("sleeps = %v, want exactly the 3 back-offs", f.sleeps)
	}
	logs := f.allLogs()
	for _, want := range []string{
		"daemon crashed 4 times in 10 minutes, not restarting",
		filepath.Join(cfg.ConfigDir, "logs"),
		"then run orch start",
	} {
		if !strings.Contains(logs, want) {
			t.Errorf("the give-up message lacks %q:\n%s", want, logs)
		}
	}
	if len(f.errors) == 0 {
		t.Errorf("giving up must be logged at ERROR level, got only: %v", f.infos)
	}
}

func TestSupervise_everyCrashAndEveryRestartIsALogLineWithTheExitCode(t *testing.T) {
	f := newFakeDaemon(t, crash(1), crash(3), clean())
	superviseDaemon(cfgIn(t), nil, f.deps())
	logs := f.allLogs()
	for _, want := range []string{
		"daemon crashed: node exited with code 1",
		"daemon crashed: node exited with code 3",
		"restarting in 2s (crash 1 of 3 allowed in 10 minutes)",
		"restarting in 10s (crash 2 of 3 allowed in 10 minutes)",
	} {
		if !strings.Contains(logs, want) {
			t.Errorf("missing log line %q in:\n%s", want, logs)
		}
	}
}

func TestSupervise_crashesFarApartAreNeverEnoughToGiveUp(t *testing.T) {
	f := newFakeDaemon(t, crash(1), crash(1), crash(1), crash(1), crash(1), crash(1), clean())
	// Each run lasts longer than the window, so no crash ever has another within ten minutes.
	f.runTime = 11 * time.Minute
	if code := superviseDaemon(cfgIn(t), nil, f.deps()); code != 0 {
		t.Errorf("got %d", code)
	}
	if f.runs != 7 {
		t.Errorf("runs = %d, want 7: a daemon that crashes once an hour is restarted every time", f.runs)
	}
	for _, d := range f.sleeps {
		if d != 2*time.Second {
			t.Errorf("an isolated crash must restart after the first delay (2s), got %v in %v", d, f.sleeps)
		}
	}
}

func TestSupervise_aSignalTheLauncherForwardedIsAnIntentionalStop(t *testing.T) {
	// Whatever code the daemon reports after the launcher forwarded a stop (1, 143 = 128+SIGTERM, -1 = killed by the signal).
	for _, code := range []int{1, 143, -1} {
		f := newFakeDaemon(t, daemonExit{code: code, signalled: true})
		if got := superviseDaemon(cfgIn(t), nil, f.deps()); got != code {
			t.Errorf("code %d: launcher exited %d, want the daemon's code", code, got)
		}
		if f.runs != 1 || len(f.sleeps) != 0 {
			t.Errorf("code %d: a stop requested by signal was treated as a crash: runs=%d sleeps=%v", code, f.runs, f.sleeps)
		}
		if strings.Contains(f.allLogs(), "crashed") {
			t.Errorf("code %d: a stop requested by signal was reported as a crash:\n%s", code, f.allLogs())
		}
	}
}

// POSIX: SIGKILL (the OOM killer's) ends the process with Go's ExitCode() == -1. Nobody asked the launcher to stop, and an
// out-of-memory kill is exactly what a restart is for, so it is a crash and the daemon comes back.
func TestSupervise_anExitKilledBySignalWithoutOursIsACrashAndIsRestarted(t *testing.T) {
	f := newFakeDaemon(t, crash(-1), clean())
	cfg := cfgIn(t)
	if code := superviseDaemon(cfg, nil, f.deps()); code != 0 {
		t.Errorf("got %d", code)
	}
	if f.runs != 2 || len(f.sleeps) != 1 || f.sleeps[0] != 2*time.Second {
		t.Errorf("runs=%d sleeps=%v, want a restart after 2s", f.runs, f.sleeps)
	}
	for _, want := range []string{"code -1", "killed by a signal or forcibly terminated", "OOM"} {
		if !strings.Contains(f.allLogs(), want) {
			t.Errorf("the log must explain a -1 exit, lacks %q:\n%s", want, f.allLogs())
		}
	}
	raw, err := os.ReadFile(filepath.Join(cfg.ConfigDir, "crashed.json"))
	if err != nil || !strings.Contains(string(raw), "killed by a signal or forcibly terminated") {
		t.Errorf("the marker must explain a -1 exit (err=%v):\n%s", err, raw)
	}
}

func TestSupervise_aStopSignalDuringTheBackoffEndsTheLauncherWithoutRestarting(t *testing.T) {
	f := newFakeDaemon(t, crash(7), crash(7))
	f.interruptSleep = 1
	code := superviseDaemon(cfgIn(t), nil, f.deps())
	if code != 7 {
		t.Errorf("got %d, want the code of the crash that was waiting to be restarted (7)", code)
	}
	if f.runs != 1 {
		t.Errorf("runs = %d: the daemon was restarted after the launcher was told to stop", f.runs)
	}
	n := 0
	for _, l := range append(append(append([]string{}, f.infos...), f.warns...), f.errors...) {
		if strings.Contains(l, "stop signal received while waiting to restart") {
			n++
			if !strings.Contains(l, "not restarting") {
				t.Errorf("the line must say what happens: %q", l)
			}
		}
	}
	if n != 1 {
		t.Errorf("want exactly one log line about the interrupted back-off, got %d:\n%s", n, f.allLogs())
	}
}

func TestSleepOrSignal_returnsFalseWhenTheDelayElapses(t *testing.T) {
	start := time.Now()
	if sleepOrSignal(20 * time.Millisecond) {
		t.Errorf("reported a stop signal, none was sent")
	}
	if time.Since(start) < 20*time.Millisecond {
		t.Errorf("returned before the delay elapsed")
	}
}

func TestSupervise_customPolicy(t *testing.T) {
	f := newFakeDaemon(t, crash(1), crash(1), crash(1))
	f.runTime = time.Second
	cfg := cfgIn(t)
	cfg.CrashRestart = &CrashRestart{Delays: []time.Duration{time.Second}, MaxRestarts: 2, Window: time.Minute, StartCommand: "myapp start"}
	if code := superviseDaemon(cfg, nil, f.deps()); code != 1 {
		t.Errorf("got %d", code)
	}
	if f.runs != 3 || len(f.sleeps) != 2 || f.sleeps[0] != time.Second || f.sleeps[1] != time.Second {
		t.Errorf("runs=%d sleeps=%v, want 3 runs and the last delay reused", f.runs, f.sleeps)
	}
	if !strings.Contains(f.allLogs(), "daemon crashed 3 times in 1 minute, not restarting") || !strings.Contains(f.allLogs(), "then run myapp start") {
		t.Errorf("give-up message wrong:\n%s", f.allLogs())
	}
}

func TestSupervise_theLauncherLeavesACrashMarkerForTheStatusCommand(t *testing.T) {
	f := newFakeDaemon(t, crash(9), clean())
	cfg := cfgIn(t)
	superviseDaemon(cfg, nil, f.deps())
	raw, err := os.ReadFile(filepath.Join(cfg.ConfigDir, "crashed.json"))
	if err != nil {
		t.Fatalf("no crash marker after a crash: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("the marker is not JSON: %v\n%s", err, raw)
	}
	for _, key := range []string{"time", "kind", "error", "version", "pid", "source"} {
		if _, ok := m[key]; !ok {
			t.Errorf("marker lacks %q: %s", key, raw)
		}
	}
	if !strings.Contains(m["error"].(string), "code 9") {
		t.Errorf("the marker must carry the exit code: %s", raw)
	}
}

const daemonMarker = `{"time":"2026-10-25T00:00:30Z","kind":"uncaughtException","error":"the real cause","version":"1","pid":1}`

func writeMarker(t *testing.T, cfg Config, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(cfg.ConfigDir, "crashed.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readMarker(t *testing.T, cfg Config) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(cfg.ConfigDir, "crashed.json"))
	if err != nil {
		t.Fatalf("no crash marker: %v", err)
	}
	return string(raw)
}

func TestSupervise_aMarkerTheDaemonWroteIsNotOverwritten(t *testing.T) {
	f := newFakeDaemon(t, crash(1), clean())
	cfg := cfgIn(t)
	writeMarker(t, cfg, daemonMarker)
	superviseDaemon(cfg, nil, f.deps())
	if got := readMarker(t, cfg); got != daemonMarker {
		t.Errorf("the daemon's own report (with the cause) was replaced by the launcher's:\n%s", got)
	}
}

// The launcher's own report of crash 1 stays when nobody clears it. Crash 2 is a new event: the status command must show it.
func TestSupervise_aLaterCrashReplacesTheLaunchersOwnStaleMarker(t *testing.T) {
	f := newFakeDaemon(t, crash(1), crash(5), clean())
	cfg := cfgIn(t)
	superviseDaemon(cfg, nil, f.deps())
	got := readMarker(t, cfg)
	if !strings.Contains(got, "code 5") || strings.Contains(got, "code 1 ") {
		t.Errorf("the marker still reports the first crash:\n%s", got)
	}
	if !strings.Contains(got, `"source": "launcher"`) {
		t.Errorf("the marker must say the launcher wrote it:\n%s", got)
	}
}

// Crash 1 leaves the launcher's marker; during run 2 the daemon writes its own, with the cause, then crashes: that one wins.
func TestSupervise_aReportTheDaemonWroteDuringTheRunBeatsTheLaunchersStaleOne(t *testing.T) {
	f := newFakeDaemon(t, crash(1), crash(1), clean())
	cfg := cfgIn(t)
	f.duringRun = func(run int, now time.Time) {
		if run == 1 {
			writeMarker(t, cfg, daemonMarker)
		}
	}
	superviseDaemon(cfg, nil, f.deps())
	if got := readMarker(t, cfg); got != daemonMarker {
		t.Errorf("the daemon's report written during the crashed run was replaced:\n%s", got)
	}
}

// A marker left by a previous launcher session (never cleared) must not hide a new crash either.
func TestSupervise_aMarkerFromAnEarlierLauncherSessionIsReplaced(t *testing.T) {
	f := newFakeDaemon(t, crash(9), clean())
	cfg := cfgIn(t)
	writeMarker(t, cfg, `{"time":"2026-10-24T10:00:00Z","kind":"exit","source":"launcher","error":"node exited with code 2","version":"unknown","pid":1}`)
	superviseDaemon(cfg, nil, f.deps())
	if got := readMarker(t, cfg); !strings.Contains(got, "code 9") {
		t.Errorf("a stale launcher marker hid the new crash:\n%s", got)
	}
}

// A launcher marker dated after the start of the run that crashed cannot be about an earlier run: leave it.
func TestSupervise_aLauncherMarkerNewerThanTheCrashedRunIsKept(t *testing.T) {
	f := newFakeDaemon(t, crash(9), clean())
	cfg := cfgIn(t)
	newer := `{"time":"2099-01-01T00:00:00Z","kind":"exit","source":"launcher","error":"node exited with code 2","version":"unknown","pid":1}`
	writeMarker(t, cfg, newer)
	superviseDaemon(cfg, nil, f.deps())
	if got := readMarker(t, cfg); got != newer {
		t.Errorf("a marker newer than the crashed run was replaced:\n%s", got)
	}
}

func TestSupervise_anUnreadableMarkerIsLeftAloneWithAWarningThatSaysWhatToDo(t *testing.T) {
	f := newFakeDaemon(t, crash(9), clean())
	cfg := cfgIn(t)
	writeMarker(t, cfg, "{not json")
	superviseDaemon(cfg, nil, f.deps())
	if got := readMarker(t, cfg); got != "{not json" {
		t.Errorf("a file the launcher cannot parse was overwritten:\n%s", got)
	}
	w := strings.Join(f.warns, "\n")
	if !strings.Contains(w, filepath.Join(cfg.ConfigDir, "crashed.json")) || !strings.Contains(w, "delete it") {
		t.Errorf("the warning must name the file and the action:\n%s", w)
	}
}

// CrashRestart is Go-only: launcher.config.json / a JSON document cannot turn it on (README, "Crash restart").
func TestConfig_crashRestartCannotBeSetFromJSON(t *testing.T) {
	var c Config
	if err := json.Unmarshal([]byte(`{"AppName":"x","CrashRestart":{"MaxRestarts":9},"crashRestart":{}}`), &c); err != nil {
		t.Fatal(err)
	}
	if c.CrashRestart != nil {
		t.Errorf("a JSON document enabled the crash restart: %+v", c.CrashRestart)
	}
}

func TestSupervise_aCleanStopLeavesNoMarker(t *testing.T) {
	f := newFakeDaemon(t, clean())
	cfg := cfgIn(t)
	superviseDaemon(cfg, nil, f.deps())
	if _, err := os.Stat(filepath.Join(cfg.ConfigDir, "crashed.json")); err == nil {
		t.Errorf("an intentional stop left a crash marker")
	}
}

// The real runner, with a real node: the exit code of the script comes back, so the loop above has something true to decide on.
func TestRunDaemonOnce_reportsTheRealExitCode(t *testing.T) {
	res := runNodeScript(t, "process.exit(3);")
	if res.code != 3 {
		t.Errorf("exit code = %d, want 3", res.code)
	}
	if res.pid <= 0 {
		t.Errorf("pid = %d, want the node pid", res.pid)
	}
	if res.signalled {
		t.Errorf("no signal was sent, signalled must be false")
	}
}

// A daemon killed from outside (POSIX SIGKILL / OOM-kill, Windows TerminateProcess) comes back non-zero and unsignalled,
// which is what the supervisor restarts on. (POSIX gives -1, see realsignal_unix_test.go; Windows gives 1.)
func TestRunDaemonOnce_aDaemonKilledFromOutsideIsNonZeroAndUnsignalled(t *testing.T) {
	res := runNodeScript(t, "process.kill(process.pid, 'SIGKILL');")
	if res.code == 0 || res.signalled {
		t.Errorf("code=%d signalled=%v, want a non-zero code and no launcher signal", res.code, res.signalled)
	}
}

// runNodeScript runs a real node on the script through the real runner.
func runNodeScript(t *testing.T, source string) daemonExit {
	t.Helper()
	if _, err := exec.LookPath("node"); err != nil {
		t.Fatalf("node is required on PATH to test the real runner (the repo's CI workflow does not run the Go tests, so run them locally with node installed): %v", err)
	}
	dir := t.TempDir()
	// The launcher's log file is a process-wide handle on <dir>/logs; Windows cannot delete the dir while it is open.
	// Registered after TempDir, so it runs before the dir is removed.
	t.Cleanup(func() {
		logMu.Lock()
		defer logMu.Unlock()
		if logFile != nil {
			_ = logFile.Close()
			logFile = nil
			logDay = ""
		}
	})
	script := filepath.Join(dir, "daemon.cjs")
	if err := os.WriteFile(script, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	return runDaemonOnce(Config{ConfigDir: dir, NodeScript: script, AppName: "test"}, nil)
}
