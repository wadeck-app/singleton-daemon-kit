# singleton-daemon-kit

Wraps a Node.js CLI daemon in a named binary (`flow.exe`, `wdrive.exe`, etc.) so it appears by name in Task Manager instead of `node.exe`.

## Key files

| Path | Purpose |
|---|---|
| `src/` | TypeScript API consumed by Node.js consumers |
| `go-launcher/` | Go package + `build.sh` that produces the launcher binary |
| `go-launcher/launcher.go` | Core launcher logic — read this before touching Go code |
| `ci/scripts/compute-version.sh` | Version computation (CI only) |

## Versioning — never bump manually

Version format: `1.YYYYMMDDHHMMSS.BUILD` (e.g. `1.20260820143022.320`), computed by CI.  
`npm version` is blocked locally via the `preversion` script — only runs in CI (`$CI=true`).  
Commit code changes only; CI handles version and publish.

## Go launcher: how consumers use it

```bash
bash go-launcher/build.sh <launcher.config.json> <outputDir> [<main.go.tmpl>]
```

`launcher.config.json` fields:

| Field | Description |
|---|---|
| `appName` | Binary name prefix (e.g. `flow`) |
| `nodeScript` | Path to the `.cjs` bundle relative to the launcher binary |
| `defaultConfigDir` | Config directory name (e.g. `.flow-cli`) |
| `cliFlags` | Flags that HTTP-dispatch to a running daemon instead of spawning node |

`LAUNCHER_BUNDLE_OVERRIDE` env var: if set, overrides `nodeScript` at runtime.  
Use this when the launcher binary and the `.cjs` bundle are in different npm packages (exe-in-npm distribution pattern).

## Two launcher modes (auto-selected from args)

- **CLI dispatch** — a flag in `cliFlags` is present: sends `POST /<command>` to the running daemon, exits. Node is never spawned.
- **Daemon mode** — no `cliFlags` flag: spawns `node <script> <args>`, manages lifecycle.

`--help` and `--version` are NOT in `cliFlags` — they pass through to Node.

## Contributing

Run `npm run check` before committing (`tsc --noEmit && vitest run`).  
Go changes: run `go test ./go-launcher/...` from the repo root.
