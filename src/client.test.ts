import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createDaemonClient } from './client.js';
import { createTestDaemon } from './test-harness.js';
import { writePortFile } from './port-file.js';
import { DaemonNotRunningError, DaemonVersionError, DaemonPortExhaustedError, type CommandMap } from './types.js';
import * as fs from 'fs/promises';
import * as os from 'os';
import * as path from 'path';
import * as crypto from 'crypto';
import * as net from 'net';

let tmpDir: string;

beforeEach(async () => {
  tmpDir = path.join(os.tmpdir(), crypto.randomUUID());
  await fs.mkdir(tmpDir, { recursive: true });
});

afterEach(async () => {
  await fs.rm(tmpDir, { recursive: true, force: true });
});

describe('client', () => {
  it('T16: send() with no port file and no local handler → DaemonNotRunningError', async () => {
    const commands = {} as CommandMap;
    const client = createDaemonClient({ configDir: tmpDir, commands });
    await expect(client.send('foo')).rejects.toThrow(DaemonNotRunningError);
    await expect(client.send('foo')).rejects.toThrow(/no port file found/);
  });

  it('T16b: send() with no daemon but local handler defined → executes locally, returns result', async () => {
    const commands = { 'check-update': async () => ({ update_available: true, version: '9.9.9' }) } as unknown as CommandMap;
    const client = createDaemonClient({ configDir: tmpDir, commands });
    const result = await client.send('check-update') as { update_available: boolean; version: string };
    expect(result.update_available).toBe(true);
    expect(result.version).toBe('9.9.9');
  });

  it('T16c: send() with no daemon and no handler for that command → DaemonNotRunningError', async () => {
    const commands = { 'check-update': async () => ({ update_available: false }) } as unknown as CommandMap;
    const client = createDaemonClient({ configDir: tmpDir, commands });
    await expect(client.send('unknown-cmd')).rejects.toThrow(DaemonNotRunningError);
  });

  it('T17: sdkVersion major mismatch (daemon lower) → DaemonVersionError', async () => {
    // Write a port file with sdkVersion 0 (lower than current 1)
    const portFilePath = path.join(tmpDir, 'config.port');
    await fs.writeFile(portFilePath, JSON.stringify({
      sdkVersion: 0,
      port: 12345,
      pid: process.pid,
      startedAt: new Date().toISOString(),
    }));
    await fs.writeFile(path.join(tmpDir, 'health_token'), 'tok');

    const commands = {} as CommandMap;
    const client = createDaemonClient({ configDir: tmpDir, commands });
    await expect(client.send('foo')).rejects.toThrow(DaemonVersionError);
  });

  it('T18: port auto-increment - if base port occupied, daemon binds on next port', async () => {
    // Occupy a port
    const blocker = net.createServer();
    await new Promise<void>((resolve) => blocker.listen(0, '127.0.0.1', resolve));
    const blockedPort = (blocker.address() as { port: number }).port;

    const commands = { ping: () => 'pong' } as unknown as CommandMap;
    await using daemon = await createTestDaemon({ commands, port: blockedPort });
    // Daemon should have picked a different port
    expect(daemon.port).not.toBe(blockedPort);
    expect(daemon.port).toBeGreaterThan(0);

    blocker.close();
  });

  it('T19: all 11 ports occupied → DaemonPortExhaustedError', async () => {
    // Occupy 11 consecutive ports by starting 11 servers
    const basePort = 49000;
    const servers: net.Server[] = [];
    for (let i = 0; i < 11; i++) {
      const s = net.createServer();
      await new Promise<void>((resolve, reject) => {
        s.listen(basePort + i, '127.0.0.1', resolve);
        s.on('error', reject);
      });
      servers.push(s);
    }

    const commands = {} as CommandMap;
    await expect(
      createTestDaemon({ commands, port: basePort })
    ).rejects.toThrow(DaemonPortExhaustedError);

    for (const s of servers) {
      await new Promise<void>((resolve) => s.close(() => resolve()));
    }
  });

  it('T20: createTestDaemon → [Symbol.asyncDispose] cleans up tmpdir', async () => {
    const commands = { ping: () => 'pong' } as unknown as CommandMap;
    let daemonConfigDir: string;

    {
      await using handle = await createTestDaemon({ commands });
      daemonConfigDir = handle.configDir;
      // Verify it exists while in scope
      const stat = await fs.stat(daemonConfigDir);
      expect(stat.isDirectory()).toBe(true);
    }

    // After dispose, tmpdir should be gone
    await expect(fs.stat(daemonConfigDir)).rejects.toThrow();
  });
});
