# singleton-daemon-kit

TypeScript SDK for the singleton daemon pattern - port file, process takeover, health HTTP server, and CLI command dispatch.

## Installation

Add the private registry to `~/.npmrc`:

```
@wadeck:registry=https://gitlab.com/api/v4/packages/npm/
//gitlab.com/api/v4/packages/npm/:_authToken=<your-read-token>
```

Then install:

```sh
npm install @wadeck/singleton-daemon-kit
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

Packages are hosted on **GitLab Packages** - source code remains on GitHub.

| Item | Value |
|------|-------|
| Registry URL | `https://gitlab.com/api/v4/packages/npm/` |
| GitLab project (namespace only) | `https://gitlab.com/wadeck/npm-registry` |
| Scope | `@wadeck` |
| Install token | GitLab deploy token with `read_package_registry` scope |
| Publish token | GitLab deploy token with `write_package_registry` scope |

### Local ~/.npmrc setup

```
@wadeck:registry=https://gitlab.com/api/v4/packages/npm/
//gitlab.com/api/v4/packages/npm/:_authToken=<read-token>
auth-type=legacy
```

### CI secrets (GitHub Actions)

| Repo | Secret | Token type |
|------|--------|------------|
| `Wadeck/singleton-daemon-kit` | `GITLAB_NPM_WRITE_TOKEN` | write deploy token |
| `Wadeck/wdrive` | `GITLAB_NPM_READ_TOKEN` | read deploy token |
| `Wadeck/agent-fleet` | `GITLAB_NPM_READ_TOKEN` | read deploy token |

Publishing happens automatically on every push to `main` via `.github/workflows/publish.yml`.

## Development

```sh
npm run check   # typecheck + 37 tests
npm run build   # compile TypeScript
```
