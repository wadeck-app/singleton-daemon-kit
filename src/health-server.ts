import * as http from 'http';
import * as net from 'net';
import * as crypto from 'crypto';
import * as fs from 'fs/promises';
import * as path from 'path';
import { DaemonPortExhaustedError, type CommandMap, type DaemonHooks, type HealthStatus } from './types.js';

const SDK_VERSION = 1;
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
  if (provided.length !== stored.length) return false;
  try {
    const a = Buffer.from(provided, 'utf8');
    const b = Buffer.from(stored, 'utf8');
    if (a.length !== b.length) return false;
    return crypto.timingSafeEqual(a, b);
  } catch {
    return false;
  }
}

export interface HealthServerOptions<T extends CommandMap> {
  configDir: string;
  commands: T;
  port?: number;
  health?: () => HealthStatus;
  versionExtra?: () => Record<string, unknown>;
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
  const { configDir, commands, health, versionExtra, hooks, onQuit } = options;
  const basePort = options.port ?? 47823;

  // Generate and write health_token
  const token = crypto.randomBytes(16).toString('hex'); // 32 hex chars
  const tokenPath = path.join(configDir, 'health_token');
  await fs.writeFile(tokenPath, token, { mode: 0o600 });

  const server = http.createServer(async (req, res) => {
    const url = req.url ?? '/';
    const method = req.method ?? 'GET';

    // POST /quit - built-in eviction endpoint, auth required
    if (method === 'POST' && url === '/quit') {
      const authHeader = req.headers['authorization'] ?? '';
      const storedToken = (await fs.readFile(tokenPath, 'utf8').catch(() => '')).trim();
      const provided = authHeader.replace(/^Bearer\s+/, '');
      if (!checkToken(provided, storedToken)) {
        sendJson(res, 401, { error: 'Unauthorized' });
        return;
      }
      sendJson(res, 200, { ok: true });
      if (onQuit) {
        // Defer quit so response is fully sent first
        setImmediate(() => { void Promise.resolve(onQuit()); });
      }
      return;
    }

    // GET /version - no auth
    if (method === 'GET' && url === '/version') {
      sendJson(res, 200, {
        ...(versionExtra ? versionExtra() : {}),
        version: PACKAGE_VERSION,
        pid: process.pid,
        config_dir: configDir,
        sdkVersion: SDK_VERSION,
        port: (server.address() as net.AddressInfo).port,
      });
      return;
    }

    // GET /health - auth required
    if (method === 'GET' && url === '/health') {
      if (!health) {
        sendJson(res, 404, { error: 'No health handler configured' });
        return;
      }
      const authHeader = req.headers['authorization'] ?? '';
      const storedToken = (await fs.readFile(tokenPath, 'utf8').catch(() => '')).trim();
      const provided = authHeader.replace(/^Bearer\s+/, '');
      if (!checkToken(provided, storedToken)) {
        sendJson(res, 401, { error: 'Unauthorized' });
        return;
      }
      sendJson(res, 200, health());
      return;
    }

    // POST /:command - auth required
    if (method === 'POST') {
      const commandName = url.slice(1); // remove leading /
      const authHeader = req.headers['authorization'] ?? '';
      const storedToken = (await fs.readFile(tokenPath, 'utf8').catch(() => '')).trim();
      const provided = authHeader.replace(/^Bearer\s+/, '');

      if (!checkToken(provided, storedToken)) {
        sendJson(res, 401, { error: 'Unauthorized' });
        return;
      }

      const handler = commands[commandName];
      if (!handler) {
        sendJson(res, 404, { error: `Unknown command: ${commandName}` });
        return;
      }

      // Parse payload
      let payload: unknown;
      const body = await readBody(req);
      if (body) {
        try { payload = JSON.parse(body); } catch { payload = undefined; }
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
          sendJson(res, 500, { error: `Command failed: ${error.message}` });
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
        if (e.code === 'EADDRINUSE' && i < maxAttempts - 1) continue;
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
        server.close((err) => {
          if (err) reject(err);
          else resolve();
        });
      });
    },
  };
}
