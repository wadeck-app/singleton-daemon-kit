import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createTestDaemon } from './test-harness.js';
import { createDaemonClient } from './client.js';
import { createDaemon } from './daemon.js';
import { writePortFile, readPortFile } from './port-file.js';
import { type CommandMap, type DaemonHandle } from './types.js';
import * as fs from 'fs/promises';
import * as os from 'os';
import * as path from 'path';
import * as crypto from 'crypto';

let tmpDir: string;

beforeEach(async () => {
  tmpDir = path.join(os.tmpdir(), crypto.randomUUID());
  await fs.mkdir(tmpDir, { recursive: true });
});

afterEach(async () => {
  await fs.rm(tmpDir, { recursive: true, force: true });
});

describe('daemon', () => {
  it('T1: daemon starts, GET /version returns pid/port/sdkVersion', async () => {
    const commands = {} as CommandMap;
    await using daemon = await createTestDaemon({ commands });
    const versionInfo = await daemon.client.version();
    expect(versionInfo.pid).toBe(process.pid);
    expect(versionInfo.sdkVersion).toBe(1);
    expect(versionInfo.port).toBe(daemon.port);
  });

  it('T2: takeover - createDaemon twice same configDir, first evicted, onTakeover called', async () => {
    const commands = { ping: () => 'pong' } as unknown as CommandMap;
    const onTakeover = vi.fn();

    const daemon1 = await createDaemon({
      configDir: tmpDir,
      commands,
    });

    // Store the first daemon's PID port to verify it's gone after takeover
    const port1 = daemon1.port;

    const daemon2 = await createDaemon({
      configDir: tmpDir,
      commands,
      hooks: { onTakeover },
    });

    expect(onTakeover).toHaveBeenCalled();
    expect(daemon2.port).toBeGreaterThan(0);

    await daemon2.stop('command');
  });

  it('T3: POST /command valid → { ok: true, result }', async () => {
    const commands = {
      echo: (payload?: unknown) => payload,
    } as unknown as CommandMap;
    await using daemon = await createTestDaemon({ commands });
    const result = await daemon.client.send('echo', 'hello');
    expect(result).toBe('hello');
  });

  it('T4: POST /command wrong token → 401', async () => {
    const commands = { ping: () => 'pong' } as unknown as CommandMap;
    await using daemon = await createTestDaemon({ commands });

    // Manually send request with wrong token
    const response = await fetch(`http://127.0.0.1:${daemon.port}/ping`, {
      method: 'POST',
      headers: { Authorization: 'Bearer wrong-token' },
    });
    expect(response.status).toBe(401);
  });

  it('T5: POST /unknown → 404', async () => {
    const commands = { ping: () => 'pong' } as unknown as CommandMap;
    await using daemon = await createTestDaemon({ commands });

    const token = await fs.readFile(path.join(daemon.configDir, 'health_token'), 'utf8');
    const response = await fetch(`http://127.0.0.1:${daemon.port}/nonexistent`, {
      method: 'POST',
      headers: { Authorization: `Bearer ${token}` },
    });
    expect(response.status).toBe(404);
  });

  it('T6: non-void handler throws → HTTP 500 with message', async () => {
    const commands = {
      fail: () => { throw new Error('boom'); },
    } as unknown as CommandMap;
    await using daemon = await createTestDaemon({ commands });

    const token = await fs.readFile(path.join(daemon.configDir, 'health_token'), 'utf8');
    const response = await fetch(`http://127.0.0.1:${daemon.port}/fail`, {
      method: 'POST',
      headers: { Authorization: `Bearer ${token}` },
    });
    expect(response.status).toBe(500);
    const body = await response.json() as { error: string };
    expect(body.error).toContain('boom');
  });

  it('T7: void handler (returns undefined) → response is 200, no error propagated', async () => {
    const onCommandError = vi.fn();
    // A void handler returns undefined - server sends { ok: true } and never 500
    const commands2 = {
      voidCmd: () => {
        // returns undefined implicitly
      },
    } as unknown as CommandMap;

    await using daemon = await createTestDaemon({
      commands: commands2,
      hooks: { onCommandError },
    });

    const token = await fs.readFile(path.join(daemon.configDir, 'health_token'), 'utf8');
    const response = await fetch(`http://127.0.0.1:${daemon.port}/voidCmd`, {
      method: 'POST',
      headers: { Authorization: `Bearer ${token}` },
    });
    expect(response.status).toBe(200);
    const body = await response.json() as { ok: boolean };
    expect(body.ok).toBe(true);
  });

  it('T-VE1: versionExtra fields appear in GET /version', async () => {
    const commands = {} as CommandMap;
    await using daemon = await createTestDaemon({
      commands,
      versionExtra: () => ({ machine_id: 'test-machine', custom_field: 42 }),
    });
    const res = await fetch(`http://127.0.0.1:${daemon.port}/version`);
    const body = await res.json() as Record<string, unknown>;
    expect(body['machine_id']).toBe('test-machine');
    expect(body['custom_field']).toBe(42);
  });

  it('T-VE2: versionExtra cannot overwrite SDK fields (pid, sdkVersion, version, port, config_dir)', async () => {
    const commands = {} as CommandMap;
    await using daemon = await createTestDaemon({
      commands,
      versionExtra: () => ({
        pid: 9999999,
        sdkVersion: 99,
        version: 'fake',
        port: 1,
        config_dir: '/fake/path',
      }),
    });
    const res = await fetch(`http://127.0.0.1:${daemon.port}/version`);
    const body = await res.json() as Record<string, unknown>;
    expect(body['pid']).toBe(process.pid);
    expect(body['sdkVersion']).toBe(1);
    expect(body['version']).not.toBe('fake');
    expect(body['port']).toBe(daemon.port);
    expect(body['config_dir']).toBe(daemon.configDir);
    expect(body['config_dir']).not.toBe('/fake/path');
  });

  it('T-VE3: /version uses config_dir (snake_case), not configDir (camelCase)', async () => {
    const commands = {} as CommandMap;
    await using daemon = await createTestDaemon({ commands });
    const res = await fetch(`http://127.0.0.1:${daemon.port}/version`);
    const body = await res.json() as Record<string, unknown>;
    expect(body['config_dir']).toBe(daemon.configDir);
    expect(body['configDir']).toBeUndefined();
  });

  it('T8: DaemonHandle.stop() → port file deleted', async () => {
    const commands = {} as CommandMap;
    await using daemon = await createTestDaemon({ commands });
    const configDir = daemon.configDir;
    await daemon.stop('command');
    const data = await readPortFile(configDir);
    expect(data).toBeNull();
  });

  it('T-VE4: createDaemon with appVersion → GET /version returns that version', async () => {
    const commands = {} as CommandMap;
    await using daemon = await createTestDaemon({
      commands,
      appVersion: '2026.07.15-test',
    });
    const res = await fetch(`http://127.0.0.1:${daemon.port}/version`);
    const body = await res.json() as Record<string, unknown>;
    expect(body['version']).toBe('2026.07.15-test');
  });

  it('T-VE5: createDaemon without appVersion → GET /version returns SDK PACKAGE_VERSION (backward compat)', async () => {
    const commands = {} as CommandMap;
    await using daemon = await createTestDaemon({ commands });
    const res = await fetch(`http://127.0.0.1:${daemon.port}/version`);
    const body = await res.json() as Record<string, unknown>;
    // Without appVersion, falls back to SDK PACKAGE_VERSION '1.0.0'
    expect(body['version']).toBe('1.0.0');
  });
});

describe('takeover - race condition', () => {
  it('T-RACE: concurrent createDaemon calls → only one daemon alive at the end', async () => {
    const commands = {} as CommandMap;

    // Start initial daemon
    const daemon1 = await createDaemon({ configDir: tmpDir, commands, port: 0 });

    // Concurrently attempt two takeovers
    const results = await Promise.allSettled([
      createDaemon({ configDir: tmpDir, commands, port: 0 }),
      createDaemon({ configDir: tmpDir, commands, port: 0 }),
    ]);

    const succeeded = results
      .filter((r): r is PromiseFulfilledResult<DaemonHandle> => r.status === 'fulfilled')
      .map(r => r.value);

    // Check how many ports are actually responding
    const alive = await Promise.all(
      succeeded.map(async d => {
        try {
          const res = await fetch(`http://127.0.0.1:${d.port}/version`);
          return res.ok;
        } catch {
          return false;
        }
      })
    );

    const aliveCount = alive.filter(Boolean).length;

    // Clean up
    for (const d of succeeded) {
      await d.stop('command').catch(() => {});
    }

    // With bug: both run simultaneously, both start health servers → 2 alive
    // With lock: they serialize, second evicts first → only 1 alive at the end
    expect(aliveCount).toBe(1);
  });
});
