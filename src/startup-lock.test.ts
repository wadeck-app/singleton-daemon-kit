import { describe, it, expect, vi } from 'vitest';
import * as fs from 'node:fs/promises';
import * as path from 'node:path';
import * as os from 'node:os';
import * as crypto from 'node:crypto';
import { acquireStartupLock } from './startup-lock.js';
import { DaemonTakeoverError } from './types.js';

// Module-level flag used by the vi.mock factory below to simulate EPERM on fs.unlink.
// Set to true in T-EPERM-UNLINK, reset to false in its finally block.
let _simulateUnlinkEPERM = false;

// vi.mock is hoisted to the top by Vitest before any import runs.
// The factory wraps the real fs/promises module, forwarding all calls
// except unlink when _simulateUnlinkEPERM is set.
vi.mock('fs/promises', async () => {
  const actual = await vi.importActual<typeof import('fs/promises')>('fs/promises');
  return {
    ...actual,
    unlink: async (...args: Parameters<typeof actual.unlink>): ReturnType<typeof actual.unlink> => {
      if (_simulateUnlinkEPERM) {
        const err = Object.assign(new Error('EPERM: operation not permitted'), { code: 'EPERM' });
        throw err;
      }
      return actual.unlink(...args);
    },
  };
});

let tmpDir: string;

import { beforeEach, afterEach } from 'vitest';

beforeEach(async () => {
  tmpDir = path.join(os.tmpdir(), crypto.randomUUID());
  await fs.mkdir(tmpDir, { recursive: true });
});

afterEach(async () => {
  _simulateUnlinkEPERM = false;
  await fs.rm(tmpDir, { recursive: true, force: true });
});

describe('startup-lock', () => {
  // T-STALE-LOCK-UNREADABLE: when config.lock contains invalid JSON, the catch block
  // must still check the deadline and throw DaemonTakeoverError rather than spinning forever.
  // Before fix: catch { continue } bypasses the deadline → infinite spin.
  // After fix: catch { if (deadline exceeded) throw; continue } terminates within timeoutMs.
  it('T-STALE-LOCK-UNREADABLE: lock with invalid JSON throws DaemonTakeoverError within timeout', async () => {
    // Write a lock file with invalid JSON so JSON.parse always throws
    const lockPath = path.join(tmpDir, 'config.lock');
    await fs.writeFile(lockPath, '{{{', 'utf8');

    const start = Date.now();
    await expect(
      acquireStartupLock(tmpDir, 500)
    ).rejects.toThrow(DaemonTakeoverError);

    // Must have thrown well within 1s (not spun forever)
    expect(Date.now() - start).toBeLessThan(1000);
  });

  // T-EPERM-UNLINK: when isProcessAlive returns false but fs.unlink throws EPERM
  // (permissions issue), the loop must not spin forever — it must check the deadline
  // and throw DaemonTakeoverError within the configured timeout.
  // Before fix: unlink failure is silently swallowed → `continue` bypasses deadline → infinite loop.
  // After fix: deadline is checked when unlink throws non-ENOENT → throws within timeout.
  // T-LOCK-TIMEOUT-MESSAGE: the error message must contain the caller-supplied timeoutMs,
  // not the hardcoded LOCK_TIMEOUT_MS (10000).
  // Before fix: message would always say '10000ms' regardless of the timeoutMs argument.
  // After fix: message says e.g. '300ms' when acquireStartupLock(dir, 300) is called.
  it('T-LOCK-TIMEOUT-MESSAGE: error message contains effective timeoutMs (not hardcoded 10000)', async () => {
    // Write a lock file with a living PID to force timeout
    const lockPath = path.join(tmpDir, 'config.lock');
    await fs.writeFile(
      lockPath,
      JSON.stringify({ pid: process.pid, startedAt: new Date().toISOString() }),
      'utf8',
    );

    const start = Date.now();
    const err = await acquireStartupLock(tmpDir, 300).catch((e: unknown) => e);
    expect(Date.now() - start).toBeLessThan(1000);
    expect(err).toBeInstanceOf(DaemonTakeoverError);
    // Must say "300ms" not "10000ms"
    expect((err as DaemonTakeoverError).message).toContain('300ms');
    expect((err as DaemonTakeoverError).message).not.toContain('10000ms');
  });

  it('T-EPERM-UNLINK: unlink throws EPERM with dead PID → DaemonTakeoverError within timeout', async () => {
    // Write a lock file with a PID that almost certainly does not exist
    const lockPath = path.join(tmpDir, 'config.lock');
    const deadPid = 999999999;
    await fs.writeFile(
      lockPath,
      JSON.stringify({ pid: deadPid, startedAt: new Date().toISOString() }),
      'utf8',
    );

    // Activate the EPERM simulation (the vi.mock factory checks this flag)
    _simulateUnlinkEPERM = true;

    try {
      const start = Date.now();
      await expect(
        acquireStartupLock(tmpDir, 200),
      ).rejects.toThrow(DaemonTakeoverError);
      // Must have thrown well within 1s (not spun forever)
      expect(Date.now() - start).toBeLessThan(1000);
    } finally {
      _simulateUnlinkEPERM = false;
      // Manually write the lock file since unlink was blocked — needed for afterEach to clean up
      await fs.writeFile(lockPath, '{}', 'utf8').catch(() => {});
    }
  });
});
