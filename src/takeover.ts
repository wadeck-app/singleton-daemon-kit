import * as http from 'http';
import * as net from 'net';
import { readPortFile, deletePortFile, isFresh } from './port-file.js';
import { DaemonTakeoverError, type DaemonHooks } from './types.js';
import * as fs from 'fs/promises';
import * as path from 'path';

function isProcessAlive(pid: number): boolean {
  try {
    process.kill(pid, 0);
    return true;
  } catch (err: unknown) {
    const e = err as NodeJS.ErrnoException;
    if (e.code === 'ESRCH') return false;
    // EPERM means process exists but we don't have permission
    if (e.code === 'EPERM') return true;
    return false;
  }
}

async function pollUntilDead(pid: number, timeoutMs: number): Promise<boolean> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (!isProcessAlive(pid)) return true;
    await new Promise(resolve => setTimeout(resolve, 100));
  }
  return !isProcessAlive(pid);
}

function isPortOpen(port: number): Promise<boolean> {
  return new Promise(resolve => {
    const socket = new net.Socket();
    socket.setTimeout(500);
    socket.once('connect', () => { socket.destroy(); resolve(true); });
    socket.once('error', () => resolve(false));
    socket.once('timeout', () => { socket.destroy(); resolve(false); });
    socket.connect(port, '127.0.0.1');
  });
}

async function pollUntilPortClosed(port: number, timeoutMs: number): Promise<boolean> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (!(await isPortOpen(port))) return true;
    await new Promise(resolve => setTimeout(resolve, 100));
  }
  return !(await isPortOpen(port));
}

function postQuit(port: number, token: string): Promise<void> {
  return new Promise((resolve, reject) => {
    const timeout = setTimeout(() => reject(new Error('timeout')), 3000);
    const req = http.request(
      { hostname: '127.0.0.1', port, method: 'POST', path: '/quit',
        headers: { Authorization: `Bearer ${token}`, 'Content-Length': '0' } },
      (res) => {
        clearTimeout(timeout);
        res.resume();
        resolve();
      }
    );
    req.on('error', (err) => { clearTimeout(timeout); reject(err); });
    req.end();
  });
}

export async function takeoverIfRunning(configDir: string, hooks: DaemonHooks): Promise<void> {
  // Step 1: read port file → null = no prior instance
  const data = await readPortFile(configDir);
  if (!data) return;

  const { pid, port } = data;

  // Step 2: if different process and not alive → stale
  if (pid !== process.pid && !isProcessAlive(pid)) {
    await deletePortFile(configDir);
    return;
  }

  // Step 3: stale mtime → stale
  if (!(await isFresh(configDir))) {
    await deletePortFile(configDir);
    return;
  }

  // Step 4: alive (or same process) → read health_token, POST /quit
  let token: string;
  try {
    token = (await fs.readFile(path.join(configDir, 'health_token'), 'utf8')).trim();
  } catch {
    token = '';
  }

  try {
    await postQuit(port, token);
  } catch {
    // Ignore errors — will check if port closed anyway
  }

  // Step 5: poll port closure every 100ms for 3s (works for both same-process and cross-process)
  const portClosed = await pollUntilPortClosed(port, 3000);
  if (portClosed) {
    await deletePortFile(configDir);
    hooks.onTakeover?.(pid);
    return;
  }

  // Step 6: port still open, different process → send SIGTERM, wait 3s
  if (pid !== process.pid) {
    try { process.kill(pid, 'SIGTERM'); } catch { /* ignore */ }
    const deadAfterSigterm = await pollUntilDead(pid, 3000);
    if (deadAfterSigterm) {
      await deletePortFile(configDir);
      hooks.onTakeover?.(pid);
      return;
    }
  }

  // Step 7: still alive → onTakeoverFailed, throw
  hooks.onTakeoverFailed?.(pid);
  throw new DaemonTakeoverError(`Failed to evict daemon with PID ${pid}`);
}
