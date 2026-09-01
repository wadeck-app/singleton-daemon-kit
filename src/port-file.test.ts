import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { writePortFile, readPortFile, isFresh, deletePortFile } from './port-file.js';
import * as fs from 'node:fs/promises';
import * as os from 'node:os';
import * as path from 'node:path';
import * as crypto from 'node:crypto';

let tmpDir: string;

beforeEach(async () => {
  tmpDir = path.join(os.tmpdir(), crypto.randomUUID());
  await fs.mkdir(tmpDir, { recursive: true });
});

afterEach(async () => {
  await fs.rm(tmpDir, { recursive: true, force: true });
});

describe('port-file', () => {
  it('T11: writePortFile + readPortFile roundtrip', async () => {
    await writePortFile(tmpDir, 12345, 9999);
    const data = await readPortFile(tmpDir);
    expect(data).not.toBeNull();
    expect(data!.port).toBe(12345);
    expect(data!.pid).toBe(9999);
    expect(data!.sdkVersion).toBe(1);
    expect(typeof data!.startedAt).toBe('string');
  });

  it('T12: isFresh returns true immediately after write', async () => {
    await writePortFile(tmpDir, 12345, 9999);
    expect(await isFresh(tmpDir)).toBe(true);
  });

  it('T13: isFresh returns false after artificially aging the mtime', async () => {
    await writePortFile(tmpDir, 12345, 9999);
    // Age the file by 70 seconds
    const oldTime = new Date(Date.now() - 70_000);
    const filePath = path.join(tmpDir, 'config.port');
    await fs.utimes(filePath, oldTime, oldTime);
    expect(await isFresh(tmpDir)).toBe(false);
  });

  it('readPortFile returns null if file absent', async () => {
    const data = await readPortFile(tmpDir);
    expect(data).toBeNull();
  });

  it('deletePortFile removes the file', async () => {
    await writePortFile(tmpDir, 12345, 9999);
    await deletePortFile(tmpDir);
    expect(await readPortFile(tmpDir)).toBeNull();
  });
});
