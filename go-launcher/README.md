# go-launcher

Go package that wraps a Node.js CLI daemon in a named `.exe`.

## Why

On Windows, all Node.js processes appear as `node.exe` in Task Manager.
This package produces a properly named binary (`wdrive.exe`, `flow-cli.exe`, etc.)
that is visible by name, while keeping Node.js as the actual daemon runtime.

## What it does

Two modes, selected automatically from `os.Args`:

- **CLI dispatch** — when a recognised flag (`--quit`, `--sync-now`, ...) is present:
  reads `config.port` + `health_token`, sends `POST /<command>` to the daemon, prints
  the result, exits. Node.js is never spawned.

- **Daemon mode** — no recognised flag: spawns `node <script> <args>` with
  `CREATE_NO_WINDOW` (no console popup), attaches a Windows Job Object so that
  killing the wrapper also kills node, forwards `Ctrl+C` from the terminal to node,
  stays alive until node exits, and forwards the exit code.

## Usage

```go
package main

import (
    "os"
    "path/filepath"
    launcher "wadeck.ch/singleton-daemon-kit/go-launcher"
)

func main() {
    exe, _ := os.Executable()
    exeDir := filepath.Dir(exe)

    // --config <dir> is read automatically; falls back to ~/.myapp
    configDir := launcher.ResolveConfigDir(os.Args[1:], launcher.DefaultConfigDir("myapp"))

    launcher.Run(launcher.Config{
        ConfigDir:  configDir,
        NodeScript: filepath.Join(exeDir, "myapp.cjs"),
        CLIFlags: []string{
            "--quit", "--restart", "--sync-now",
        },
    })
}
```

## Task Manager result

```
myapp.exe   (Go wrapper, ~0% CPU)
node.exe    (child — the actual daemon)
```

Killing `myapp.exe` kills `node.exe` automatically (Job Object).
Killing `node.exe` causes `myapp.exe` to exit naturally (`cmd.Wait()` returns).

## Logging

Writes to `<configDir>/logs/YYYY-MM-DD-launcher.log`.
Format: `[15:28:46] [ INFO] [launcher] message`
Node.js's 30-day log rotation covers these files too.

## Cross-compilation

```bash
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o dist/myapp_windows.exe
GOOS=darwin  GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o dist/myapp_darwin_arm64
GOOS=darwin  GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o dist/myapp_darwin_amd64
```

All three targets compile from a single `ubuntu-latest` CI runner with `CGO_ENABLED=0`.
No macOS SDK headers required.
