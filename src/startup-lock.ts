import * as fs from 'fs/promises';
import * as path from 'path';
import { DaemonTakeoverError } from './types.js';

const LOCK_RETRY_INTERVAL_MS = 100;
const LOCK_TIMEOUT_MS = 10_000;

interface LockData {
  pid: number;
  startedAt: string;
}

function isProcessAlive(pid: number): boolean {
  try {
    process.kill(pid, 0);
    return true;
  } catch (err: unknown) {
    const e = err as NodeJS.ErrnoException;
    if (e.code === 'ESRCH') return false;
    // EPERM: process exists but we lack permission
    if (e.code === 'EPERM') return true;
    return false;
  }
}

/**
 * Acquires an exclusive file-based startup lock for the given configDir.
 * Uses fs.open with flag 'wx' (O_CREAT | O_EXCL) — atomic on POSIX and Windows.
 *
 * Prevents two concurrent createDaemon calls on the same configDir from both
 * running takeoverIfRunning simultaneously (race condition where both evict the
 * existing daemon and both start their own health server).
 *
 * Returns a release function. Must be called in a finally block after the port
 * file is written and the server is listening.
 *
 * Throws DaemonTakeoverError if the lock cannot be acquired within LOCK_TIMEOUT_MS.
 */
export async function acquireStartupLock(configDir: string): Promise<() => Promise<void>> {
  const lockPath = path.join(configDir, 'config.lock');
  const deadline = Date.now() + LOCK_TIMEOUT_MS;

  while (true) {
    try {
      // 'wx' = O_CREAT | O_EXCL: atomic exclusive create — EEXIST if file already exists
      const fd = await fs.open(lockPath, 'wx');
      const data: LockData = { pid: process.pid, startedAt: new Date().toISOString() };
      await fd.writeFile(JSON.stringify(data), 'utf8');
      await fd.close();

      return async (): Promise<void> => {
        try {
          await fs.unlink(lockPath);
        } catch {
          // Ignore: file may already be gone
        }
      };
    } catch (err: unknown) {
      const e = err as NodeJS.ErrnoException;
      if (e.code !== 'EEXIST') throw err;

      // Lock file exists — check if the owner process is still alive
      try {
        const content = await fs.readFile(lockPath, 'utf8');
        const { pid } = JSON.parse(content) as LockData;
        if (!isProcessAlive(pid)) {
          // Owner is dead — remove stale lock and retry immediately
          await fs.unlink(lockPath).catch(() => {});
          continue;
        }
        // Owner is alive — fall through to timed retry
      } catch {
        // Lock file disappeared or is unreadable (another process deleted it) — retry
        continue;
      }

      if (Date.now() >= deadline) {
        throw new DaemonTakeoverError(
          `Could not acquire startup lock in ${configDir} within ${LOCK_TIMEOUT_MS}ms`
        );
      }

      await new Promise<void>(resolve => setTimeout(resolve, LOCK_RETRY_INTERVAL_MS));
    }
  }
}
