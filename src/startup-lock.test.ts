import { describe, it, expect } from 'vitest';
import * as fs from 'fs/promises';
import * as path from 'path';
import * as os from 'os';
import * as crypto from 'crypto';
import { acquireStartupLock } from './startup-lock.js';
import { DaemonTakeoverError } from './types.js';

let tmpDir: string;

import { beforeEach, afterEach } from 'vitest';

beforeEach(async () => {
  tmpDir = path.join(os.tmpdir(), crypto.randomUUID());
  await fs.mkdir(tmpDir, { recursive: true });
});

afterEach(async () => {
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
});
