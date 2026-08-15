import * as fs from 'fs/promises';
import * as path from 'path';
import { DaemonTakeoverError } from './types.js';
import { isProcessAlive } from './process-utils.js';

const LOCK_RETRY_INTERVAL_MS = 100;
const LOCK_TIMEOUT_MS = 10_000;

interface LockData {
  pid: number;
  startedAt: string;
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
export async function acquireStartupLock(configDir: string, timeoutMs?: number): Promise<() => Promise<void>> {
  const lockPath = path.join(configDir, 'config.lock');
  const deadline = Date.now() + (timeoutMs ?? LOCK_TIMEOUT_MS);

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
          // TOCTOU note: between isProcessAlive(pid) returning false and unlink(), another
          // process may have written a fresh valid lock. The unlink removes it, but the
          // subsequent O_EXCL open then races cleanly — only one caller gets EEXIST.
          // Worst case: dual lock acquisition if the new lock is written AND deleted before
          // either caller reaches fs.open('wx'). Using OS-level flock would eliminate this
          // window but is not cross-platform. Acceptable for a single-user local daemon.
          //
          // X6 — if unlink fails with a real error (e.g. EPERM), check the deadline
          // before continuing to avoid an infinite spin. ENOENT means another process
          // already deleted it, which is fine — continue as normal.
          await fs.unlink(lockPath).catch((unlinkErr) => {
            const code = (unlinkErr as NodeJS.ErrnoException).code;
            if (code !== 'ENOENT') {
              if (Date.now() >= deadline) {
                throw new DaemonTakeoverError(
                  `Could not acquire startup lock in ${configDir} within ${timeoutMs ?? LOCK_TIMEOUT_MS}ms`
                );
              }
            }
          });
          continue;
        }
        // Owner is alive — fall through to timed retry
      } catch (innerErr) {
        // Re-throw DaemonTakeoverError from the unlink catch above — do not swallow it.
        if (innerErr instanceof DaemonTakeoverError) throw innerErr;
        // Lock file disappeared or is unreadable (another process deleted it) — retry.
        // Check deadline first to avoid spinning forever on a persistently invalid lock.
        if (Date.now() >= deadline) {
          throw new DaemonTakeoverError(
            `Could not acquire startup lock in ${configDir} within ${timeoutMs ?? LOCK_TIMEOUT_MS}ms`
          );
        }
        continue;
      }

      if (Date.now() >= deadline) {
        // Use the effective timeout (caller-supplied or default) in the message.
        throw new DaemonTakeoverError(
          `Could not acquire startup lock in ${configDir} within ${timeoutMs ?? LOCK_TIMEOUT_MS}ms`
        );
      }

      await new Promise<void>(resolve => setTimeout(resolve, LOCK_RETRY_INTERVAL_MS));
    }
  }
}
