import * as http from 'http';
import * as fs from 'fs/promises';
import * as path from 'path';
import { readPortFile } from './port-file.js';
import {
  DaemonNotRunningError,
  DaemonVersionError,
  DaemonAuthError,
  DaemonCommandNotFoundError,
  type DaemonClient,
  type CommandMap,
  type CommandName,
  type CommandResult,
} from './types.js';
import { isProcessAlive } from './process-utils.js';
import { SDK_VERSION } from './constants.js';

// NOTE: This extends the spec (which declares createDaemonClient({ configDir })).
// The `commands` map serves two purposes:
// 1. TypeScript return-type inference for send<K>()
// 2. Local fallback execution when no daemon is running (see send() implementation)
// Consumers that want strict spec compliance can pass an empty object as commands.
interface ClientOptions<T extends CommandMap> {
  configDir: string;
  commands: T;
}

function httpPost(port: number, commandPath: string, token: string, payload?: unknown): Promise<{ status: number; body: Record<string, unknown> }> {
  return new Promise((resolve, reject) => {
    const body = payload !== undefined ? JSON.stringify(payload) : '';
    const req = http.request(
      {
        hostname: '127.0.0.1',
        port,
        method: 'POST',
        path: `/${commandPath}`,
        headers: {
          Authorization: `Bearer ${token}`,
          'Content-Type': 'application/json',
          'Content-Length': Buffer.byteLength(body),
        },
      },
      (res) => {
        let data = '';
        res.on('data', chunk => { data += chunk; });
        res.on('end', () => {
          try {
            const parsed = JSON.parse(data) as Record<string, unknown>;
            resolve({ status: res.statusCode ?? 0, body: parsed });
          } catch {
            reject(new Error(`Invalid JSON response: ${data}`));
          }
        });
      }
    );
    req.setTimeout(5000, () => {
      req.destroy(new Error('HTTP timeout after 5s'));
    });
    req.on('error', reject);
    if (body) req.write(body);
    req.end();
  });
}

function httpGet(port: number, urlPath: string): Promise<{ status: number; body: Record<string, unknown> }> {
  return new Promise((resolve, reject) => {
    const req = http.request(
      { hostname: '127.0.0.1', port, method: 'GET', path: urlPath },
      (res) => {
        let data = '';
        res.on('data', chunk => { data += chunk; });
        res.on('end', () => {
          try {
            resolve({ status: res.statusCode ?? 0, body: JSON.parse(data) as Record<string, unknown> });
          } catch {
            reject(new Error(`Invalid JSON response: ${data}`));
          }
        });
      }
    );
    req.setTimeout(5000, () => {
      req.destroy(new Error('HTTP timeout after 5s'));
    });
    req.on('error', reject);
    req.end();
  });
}

export function createDaemonClient<T extends CommandMap>(options: ClientOptions<T>): DaemonClient<T> {
  const { configDir } = options;

  return {
    async isRunning(): Promise<boolean> {
      try {
        const data = await readPortFile(configDir);
        if (!data) return false;
        return isProcessAlive(data.pid);
      } catch {
        return false;
      }
    },

    async version(): Promise<{ version: string; pid: number; config_dir: string; sdkVersion: number }> {
      const data = await readPortFile(configDir);
      if (!data) throw new DaemonNotRunningError(`Daemon is not running (no port file found in ${configDir})`);
      const resp = await httpGet(data.port, '/version');
      return resp.body as { version: string; pid: number; config_dir: string; sdkVersion: number };
    },

    async send<K extends CommandName<T>>(command: K, payload?: unknown): Promise<CommandResult<T, K>> {
      const data = await readPortFile(configDir);
      const localHandler = (options.commands as Record<string, ((...args: unknown[]) => unknown) | undefined>)[command as string];

      if (!data || !isProcessAlive(data.pid)) {
        if (localHandler) {
          return localHandler(payload) as Promise<CommandResult<T, K>>;
        }
        const reason = !data ? `no port file found in ${configDir}` : `Daemon process ${data.pid} is not running`;
        throw new DaemonNotRunningError(`Daemon is not running (${reason})`);
      }

      // Check SDK version compatibility
      // Treat missing sdkVersion (old port file) as version 0 — will fail the version guard.
      const daemonMajor = data.sdkVersion ?? 0;
      const clientMajor = SDK_VERSION;
      if (daemonMajor < clientMajor) {
        throw new DaemonVersionError(
          `Daemon SDK version ${daemonMajor} is older than client SDK version ${clientMajor}`
        );
      } else if (daemonMajor > clientMajor) {
        console.warn(`Warning: Daemon SDK version ${daemonMajor} is newer than client SDK version ${clientMajor}`);
      }

      // Read health token — ENOENT means the daemon wrote its port file but
      // the health_token is missing (crash between the two writes). Treat as not running.
      let token: string;
      try {
        token = (await fs.readFile(path.join(configDir, 'health_token'), 'utf8')).trim();
      } catch (err) {
        const code = (err as NodeJS.ErrnoException).code;
        if (code === 'ENOENT') {
          throw new DaemonNotRunningError(
            `Daemon is not running (health_token not found in ${configDir})`
          );
        }
        throw err;
      }

      const resp = await httpPost(data.port, command, token, payload).catch((err: unknown) => {
        const msg = err instanceof Error ? err.message : String(err);
        if (msg.includes('ECONNREFUSED') || msg.includes('ECONNRESET') || msg.includes('timeout')) {
          throw new DaemonNotRunningError(`Daemon is not running (connection failed: ${msg})`);
        }
        throw err;
      });

      if (resp.status === 401) throw new DaemonAuthError('Unauthorized - token mismatch');
      if (resp.status === 404) throw new DaemonCommandNotFoundError(`Unknown command: ${command}`);
      if (resp.status === 500) throw new Error((resp.body.error as string) ?? 'Command failed');

      return (resp.body as { result?: unknown }).result as CommandResult<T, K>;
    },
  };
}
