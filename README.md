# singleton-daemon-kit

TypeScript SDK for the singleton daemon pattern - port file, process takeover, health HTTP server, and CLI command dispatch.

## Installation

Add the private registry to `~/.npmrc`:

```
@wadeck-app:registry=https://npm.pkg.github.com/
//npm.pkg.github.com/:_authToken=<ghp_...>
```

Then install:

```sh
npm install @wadeck-app/singleton-daemon-kit
```

## Quick start

```typescript
import { createDaemon, createDaemonClient } from '@wadeck/singleton-daemon-kit';

// daemon process (src/daemon.ts)
const handle = await createDaemon({
  configDir: '/home/user/.myapp',
  port: 47823,
  commands: {
    ping: () => ({ pong: true }),
    quit: () => { void handle.stop('command'); },
  },
  hooks: {
    onStart: (port) => console.log(`Daemon listening on port ${port}`),
    onShutdown: (reason) => console.log(`Stopped: ${reason}`),
  },
});

// CLI process (src/main.ts)
const client = createDaemonClient({
  configDir: '/home/user/.myapp',
  commands: {} as ReturnType<typeof buildCommands>,
});

if (await client.isRunning()) {
  const result = await client.send('ping');
  console.log(result); // { pong: true }
}
```

## npm Registry

Packages are hosted on **GitHub Packages**.

| Item | Value |
|------|-------|
| Registry URL | `https://npm.pkg.github.com/` |
| GitHub repo | `https://github.com/wadeck-app/singleton-daemon-kit` |
| Scope | `@wadeck-app` |
| Install token | GitHub PAT with `read:packages` scope |
| Publish token | `GITHUB_TOKEN` (automatic in CI) |

### Local ~/.npmrc setup

```
@wadeck-app:registry=https://npm.pkg.github.com/
//npm.pkg.github.com/:_authToken=<ghp_...>
```

### CI secrets (GitHub Actions)

| Repo | Secret | Token type |
|------|--------|------------|
| `wadeck-app/singleton-daemon-kit` | `GITHUB_TOKEN` | automatic |
| `<owner>/<repo>` | PAT secret | `read:packages` PAT |

Publishing happens automatically on every push to `main` via `.github/workflows/publish.yml`.

## Go launcher — built-in flags

The Go launcher handles `--help`, `-h`, and `--version` automatically before any dispatch or daemon logic runs. Do **not** add these to `cliFlags` in your `launcher.config.json` — they are reserved.

| Flag | Behaviour |
|------|-----------|
| `--help`, `-h` | Print usage, commands, and options then exit 0 |
| `--version` | Print `<appName> version <version>` then exit 0 |

The version string is injected at build time via `-ldflags "-X main.version=<value>"` in `build.sh`. At runtime it is passed through `Config.Version`; if empty, `--version` prints `unknown`.

`Config.CLIDescriptions` maps each flag in `CLIFlags` to its one-line description shown in `--help`. Missing keys are shown with no description (no error). In `launcher.config.json`, populate the optional `cliFlagDescriptions` object.

## Development

```sh
npm run check   # typecheck + 37 tests
npm run build   # compile TypeScript
```
