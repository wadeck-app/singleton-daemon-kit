import * as http from 'http';
import * as fs from 'fs/promises';
import * as path from 'path';
import { readPortFile } from './port-file.js';
import {
  DaemonNotRunningError,
  DaemonVersionError,
  type DaemonClient,
  type CommandMap,
  type CommandName,
  type CommandResult,
} from './types.js';

const SDK_VERSION = 1;

interface ClientOptions<T extends CommandMap> {
  configDir: string;
  commands: T;
}

function isProcessAlive(pid: number): boolean {
  try {
    process.kill(pid, 0);
    return true;
  } catch (err: unknown) {
    const e = err as NodeJS.ErrnoException;
    if (e.code === 'ESRCH') return false;
    if (e.code === 'EPERM') return true;
    return false;
  }
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
      if (!data) {
        throw new DaemonNotRunningError(`Daemon is not running (no port file found in ${configDir})`);
      }

      if (!isProcessAlive(data.pid)) {
        throw new DaemonNotRunningError(`Daemon process ${data.pid} is not running`);
      }

      // Check SDK version compatibility
      const daemonMajor = data.sdkVersion;
      const clientMajor = SDK_VERSION;
      if (daemonMajor < clientMajor) {
        throw new DaemonVersionError(
          `Daemon SDK version ${daemonMajor} is older than client SDK version ${clientMajor}`
        );
      }
      if (daemonMajor > clientMajor) {
        console.warn(`Warning: Daemon SDK version ${daemonMajor} is newer than client SDK version ${clientMajor}`);
      }

      // Read health token
      const token = (await fs.readFile(path.join(configDir, 'health_token'), 'utf8')).trim();

      const resp = await httpPost(data.port, command, token, payload);

      if (resp.status === 401) throw new DaemonNotRunningError('Unauthorized — token mismatch');
      if (resp.status === 404) throw new DaemonNotRunningError(`Unknown command: ${command}`);
      if (resp.status === 500) throw new Error((resp.body.error as string) ?? 'Command failed');

      return (resp.body as { result?: unknown }).result as CommandResult<T, K>;
    },
  };
}
