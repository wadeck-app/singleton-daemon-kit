import * as http from 'node:http';
import * as net from 'node:net';
import * as crypto from 'node:crypto';
import * as fs from 'node:fs/promises';
import * as path from 'node:path';
import { DaemonPortExhaustedError, type CommandMap, type DaemonHooks, type HealthStatus } from './types.js';
import { SDK_VERSION } from './constants.js';

function getErrorMessage(e: unknown): string {
  // violations-suppress: ts/no-err-message-direct helper implementation — this IS the safe accessor
  return e instanceof Error ? e.message : String(e);
}
const PACKAGE_VERSION = '1.0.0';

async function tryListen(server: http.Server, port: number): Promise<number> {
  return new Promise((resolve, reject) => {
    const onError = (err: NodeJS.ErrnoException) => {
      server.removeListener('error', onError);
      reject(err);
    };
    server.once('error', onError);
    server.listen(port, '127.0.0.1', () => {
      server.removeListener('error', onError);
      const addr = server.address() as net.AddressInfo;
      resolve(addr.port);
    });
  });
}

function readBody(req: http.IncomingMessage): Promise<string> {
  return new Promise((resolve, reject) => {
    let body = '';
    req.on('data', chunk => { body += chunk; });
    req.on('end', () => resolve(body));
    req.on('error', reject);
  });
}

function sendJson(res: http.ServerResponse, status: number, data: unknown): void {
  const json = JSON.stringify(data);
  res.writeHead(status, { 'Content-Type': 'application/json' });
  res.end(json);
}

function checkToken(provided: string, stored: string): boolean {
  if (!provided || !stored) return false;
  // Both tokens are always 32 hex chars (randomBytes(16).toString('hex')),
  // so the length check leaks no timing information.
  if (provided.length !== stored.length) return false;
  try {
    return crypto.timingSafeEqual(Buffer.from(provided, 'utf8'), Buffer.from(stored, 'utf8'));
  } catch {
    return false;
  }
}

// Centralised auth guard — used by all authenticated routes (/quit, /health, POST commands).
// Sends 401 and returns false if the token is missing or wrong; returns true on success.
// Avoids duplicating the extraction + checkToken call across multiple routes, which would
// risk an auth bypass if one copy were missed during a future scheme change.
function requireAuth(req: http.IncomingMessage, res: http.ServerResponse, token: string): boolean {
  const authHeader = req.headers['authorization'] ?? '';
  const provided = authHeader.replace(/^Bearer\s+/, '');
  if (!checkToken(provided, token)) {
    sendJson(res, 401, { error: 'Unauthorized' });
    return false;
  }
  return true;
}

export interface HealthServerOptions<T extends CommandMap> {
  configDir: string;
  commands: T;
  port?: number;
  health?: () => HealthStatus;
  versionExtra?: () => Record<string, unknown>;
  appVersion?: string;
  hooks?: DaemonHooks;
  onQuit?: () => void | Promise<void>;
}

export interface HealthServerHandle {
  port: number;
  close(): Promise<void>;
}

export async function startHealthServer<T extends CommandMap>(
  options: HealthServerOptions<T>
): Promise<HealthServerHandle> {
  const { configDir, commands, health, versionExtra, appVersion, hooks, onQuit } = options;
  const basePort = options.port ?? 47823;

  // Generate and write health_token
  const token = crypto.randomBytes(16).toString('hex'); // 32 hex chars
  const tokenPath = path.join(configDir, 'health_token');
  await fs.writeFile(tokenPath, token, { mode: 0o600 });

  const server = http.createServer(async (req, res) => {
    // Strip query string before routing so /quit?reason=x matches /quit, etc.
    const rawUrl = req.url ?? '/';
    const url = new URL(rawUrl, 'http://localhost').pathname;
    const method = req.method ?? 'GET';

    // POST /quit - built-in eviction endpoint, auth required
    if (method === 'POST' && url === '/quit') {
      if (!requireAuth(req, res, token)) return;
      sendJson(res, 200, { ok: true });
      if (onQuit) {
        // Defer quit so response is fully sent first
        setImmediate(() => { void Promise.resolve(onQuit()); });
      }
      return;
    }

    // GET /version - no auth
    if (method === 'GET' && url === '/version') {
      // Field names use snake_case to match the JSON API convention documented in
      // CLAUDE.md ("GET /version → { version, machine_id, config_dir, pid }").
      sendJson(res, 200, {
        ...(versionExtra ? versionExtra() : {}),
        version: appVersion ?? PACKAGE_VERSION,
        pid: process.pid,
        config_dir: configDir,
        sdkVersion: SDK_VERSION,
        port: (server.address() as net.AddressInfo | null)?.port ?? actualPort,
      });
      return;
    }

    // GET /health - auth required
    if (method === 'GET' && url === '/health') {
      if (!health) {
        sendJson(res, 404, { error: 'No health handler configured' });
        return;
      }
      if (!requireAuth(req, res, token)) return;
      // Wrap health() in try/catch: an uncaught throw here would leave the
      // response unsent and cause ECONNRESET on the client side.
      try {
        sendJson(res, 200, health());
      } catch (err: unknown) {
        const error = err instanceof Error ? err : new Error(String(err));
        hooks?.onCommandError?.('health', error);
        if (!res.headersSent) {
          sendJson(res, 500, { error: `Health check failed: ${getErrorMessage(error)}` });
        }
      }
      return;
    }

    // Commands returning data (check-update, apply-update) are awaited before sending
    // the response — the caller needs the result.
    // Void commands (quit, restart, sync-now) must return quickly: their handlers
    // use `void` internally to start async work and return immediately, making them
    // effectively fire-and-forget from the HTTP perspective.
    // POST /:command - auth required
    if (method === 'POST') {
      const commandName = url.slice(1); // remove leading /
      if (!requireAuth(req, res, token)) return;

      const handler = Object.hasOwn(commands, commandName)
        ? commands[commandName as keyof T]
        : undefined;
      if (!handler) {
        sendJson(res, 404, { error: `Unknown command: ${commandName}` });
        return;
      }

      // Parse payload — reject malformed JSON with 400 rather than silently swallowing it.
      let payload: unknown = undefined;
      const body = await readBody(req);
      if (body) {
        try {
          payload = JSON.parse(body);
        } catch {
          sendJson(res, 400, { error: 'Invalid JSON body' });
          return;
        }
      }

      const start = Date.now();
      try {
        const result = await Promise.resolve(handler(payload));
        const durationMs = Date.now() - start;
        hooks?.onCommand?.(commandName, durationMs);
        if (result === undefined) {
          sendJson(res, 200, { ok: true });
        } else {
          sendJson(res, 200, { ok: true, result });
        }
      } catch (err: unknown) {
        const error = err instanceof Error ? err : new Error(String(err));
        hooks?.onCommandError?.(commandName, error);
        if (!res.headersSent) {
          sendJson(res, 500, { error: `Command failed: ${getErrorMessage(error)}` });
        }
      }
      return;
    }

    res.writeHead(404);
    res.end();
  });

  // Try ports from basePort to basePort+10
  // Special case: port 0 means OS assigns a port - don't iterate
  let actualPort: number | null = null;

  if (basePort === 0) {
    actualPort = await tryListen(server, 0);
  } else {
    const maxAttempts = 11;
    for (let i = 0; i < maxAttempts; i++) {
      const tryPort = basePort + i;
      try {
        actualPort = await tryListen(server, tryPort);
        break;
      } catch (err: unknown) {
        const e = err as NodeJS.ErrnoException;
        if (e.code !== 'EADDRINUSE') throw err; // propagate non-bind errors as-is
        if (i < maxAttempts - 1) continue;
        throw new DaemonPortExhaustedError(
          `Could not bind to any port in range ${basePort}-${basePort + maxAttempts - 1}`
        );
      }
    }
  }

  return {
    port: actualPort!,
    close(): Promise<void> {
      return new Promise((resolve, reject) => {
        // Destroy all keep-alive connections so server.close() resolves promptly.
        // Node 18.2+ provides server.closeAllConnections(); fall back to manual tracking.
        if (typeof (server as unknown as { closeAllConnections?: () => void }).closeAllConnections === 'function') {
          (server as unknown as { closeAllConnections: () => void }).closeAllConnections();
        }
        server.close((err) => {
          if (err) reject(err);
          else resolve();
        });
      });
    },
  };
}
