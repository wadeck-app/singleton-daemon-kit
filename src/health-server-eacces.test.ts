/**
 * T-NON-EADDRINUSE: verify that a non-EADDRINUSE error from listen() propagates
 * as-is out of startHealthServer(), instead of being swallowed or wrapped in
 * DaemonPortExhaustedError.
 *
 * Isolated in its own file so vi.mock('http') does not affect other test files.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import * as os from 'os';
import * as fs from 'fs/promises';
import * as path from 'path';
import * as crypto from 'crypto';
import type { CommandMap } from './types.js';

// ---------------------------------------------------------------------------
// Module-level mock: replace http.createServer with a factory that returns
// a minimal fake server which emits EACCES on listen().
// vi.mock is hoisted above imports by vitest, so this affects the module under test.
// ---------------------------------------------------------------------------
const eaccesError = Object.assign(new Error('EACCES: permission denied, bind'), { code: 'EACCES' });

vi.mock('http', async (importOriginal) => {
  const actual = await importOriginal<typeof import('http')>();

  let storedErrorCb: ((err: Error) => void) | null = null;

  const mockServer = {
    once(event: string, cb: (err: Error) => void) {
      if (event === 'error') storedErrorCb = cb;
      return this;
    },
    removeListener() { return this; },
    listen(_port: number, _host: string, _cb: () => void) {
      setImmediate(() => { if (storedErrorCb) storedErrorCb(eaccesError); });
    },
    close(cb: (err?: Error) => void) { cb(); },
    address() { return null; },
  };

  return {
    ...actual,
    createServer: vi.fn().mockReturnValue(mockServer),
  };
});

// Import after mock registration
import { startHealthServer } from './health-server.js';

let tmpDir: string;

beforeEach(async () => {
  tmpDir = path.join(os.tmpdir(), crypto.randomUUID());
  await fs.mkdir(tmpDir, { recursive: true });
});

afterEach(async () => {
  await fs.rm(tmpDir, { recursive: true, force: true });
});

describe('health-server — non-EADDRINUSE error propagation', () => {
  // T-NON-EADDRINUSE: when listen() fails with a non-EADDRINUSE error (e.g. EACCES),
  // startHealthServer must rethrow it as-is, not wrap it in DaemonPortExhaustedError.
  // Before the fix: the catch block iterated all ports and then threw DaemonPortExhaustedError.
  // After the fix: `if (e.code !== 'EADDRINUSE') throw err;` propagates them immediately.
  it('T-NON-EADDRINUSE: EACCES from listen() propagates as-is (not DaemonPortExhaustedError)', async () => {
    await expect(
      startHealthServer({ configDir: tmpDir, commands: {} as CommandMap, port: 47900 })
    ).rejects.toMatchObject({ code: 'EACCES' });
  });
});
