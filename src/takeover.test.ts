import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// ─── Delegation state: null = delegate to real implementation ─────────────────
let _httpRequestOverride: ((...args: unknown[]) => unknown) | null = null;
let _netSocketPortOpen: (() => boolean) | null = null;
// Per-test override for fs.readFile: null = use real fs
let _fsReadFileOverride: ((path: string, enc: string) => Promise<string>) | null = null;

// ─── Port-file mock ───────────────────────────────────────────────────────────

vi.mock('./port-file.js', () => ({
  readPortFile: vi.fn(),
  deletePortFile: vi.fn(),
  writePortFile: vi.fn(),
  isFresh: vi.fn(),
  startHeartbeat: vi.fn(() => () => {}),
}));

// ─── http mock ───────────────────────────────────────────────────────────────

vi.mock('http', async (importActual) => {
  const actual = await importActual<typeof import('http')>();
  return {
    ...actual,
    request: (...args: unknown[]) => {
      if (_httpRequestOverride) return _httpRequestOverride(...args);
      return (actual.request as (...a: unknown[]) => unknown)(...args);
    },
  };
});

// ─── net mock ─────────────────────────────────────────────────────────────────
// Use a plain function constructor to avoid calling real Socket() (which
// allocates a libuv handle that prevents the process from exiting cleanly).

vi.mock('net', async (importActual) => {
  const actual = await importActual<typeof import('net')>();
  function DelegatingSocket(this: unknown) {
    if (_netSocketPortOpen !== null) {
      // eslint-disable-next-line no-constructor-return
      return makeFakeSocket(_netSocketPortOpen) as unknown as typeof this;
    }
    // eslint-disable-next-line no-constructor-return
    return Reflect.construct(actual.Socket, [], DelegatingSocket);
  }
  DelegatingSocket.prototype = Object.create(actual.Socket.prototype);
  DelegatingSocket.prototype.constructor = DelegatingSocket;
  return { ...actual, Socket: DelegatingSocket as unknown as typeof actual.Socket };
});

// ─── fs/promises mock ─────────────────────────────────────────────────────────
// Delegates to real fs by default; unit tests override _fsReadFileOverride to
// eliminate real I/O so fake timers (which block real I/O) work correctly.

vi.mock('fs/promises', async (importActual) => {
  const actual = await importActual<typeof import('fs/promises')>();
  return {
    ...actual,
    readFile: (...args: unknown[]) => {
      if (_fsReadFileOverride && typeof args[1] === 'string') {
        return _fsReadFileOverride(args[0] as string, args[1]);
      }
      return (actual.readFile as (...a: unknown[]) => unknown)(...args);
    },
  };
});

// ─── Imports (after mocks) ───────────────────────────────────────────────────

import { takeoverIfRunning } from './takeover.js';
import { writePortFile, readPortFile, deletePortFile, isFresh } from './port-file.js';
import * as fsPromises from 'fs/promises';
import * as os from 'os';
import * as path from 'path';
import * as crypto from 'crypto';

const mockReadPortFile = vi.mocked(readPortFile);
const mockDeletePortFile = vi.mocked(deletePortFile);
const mockWritePortFile = vi.mocked(writePortFile);
const mockIsFresh = vi.mocked(isFresh);

// ─── Fake socket factory ─────────────────────────────────────────────────────

function makeFakeSocket(portOpen: () => boolean): object {
  const listeners: Record<string, ((...args: unknown[]) => void)[]> = {};
  return {
    setTimeout: vi.fn(),
    destroy: vi.fn(),
    once(event: string, cb: (...args: unknown[]) => void) {
      listeners[event] = listeners[event] ?? [];
      listeners[event].push(cb);
      return this;
    },
    connect(_port: number, _host: string) {
      // Use Promise.resolve().then() so listeners are registered before firing
      Promise.resolve().then(() => {
        if (portOpen()) {
          (listeners['connect'] ?? []).forEach(cb => cb());
        } else {
          (listeners['error'] ?? []).forEach(cb => cb(new Error('ECONNREFUSED')));
        }
      });
      return this;
    },
  };
}

// ─── Fake http.request factory ───────────────────────────────────────────────

type HttpCallback = (res: import('http').IncomingMessage) => void;

function makeHttpRequestMock(opts: { rejectWith?: Error } = {}) {
  return (...args: unknown[]) => {
    const callback = args.find(a => typeof a === 'function') as HttpCallback | undefined;
    const listeners: Record<string, ((...a: unknown[]) => void)[]> = {};
    const req = {
      on(event: string, cb: (...a: unknown[]) => void) {
        listeners[event] = listeners[event] ?? [];
        listeners[event].push(cb);
        return req;
      },
      end() {
        if (opts.rejectWith) {
          Promise.resolve().then(() =>
            (listeners['error'] ?? []).forEach(cb => cb(opts.rejectWith)),
          );
        } else {
          // Call response callback synchronously so postQuit resolves without
          // needing a timer tick
          const fakeRes = { resume: vi.fn(), statusCode: 200 } as unknown as import('http').IncomingMessage;
          callback?.(fakeRes);
        }
        return req;
      },
      setTimeout: () => req,
    };
    return req;
  };
}

// ─── Global setup ────────────────────────────────────────────────────────────

// Restore real port-file implementations so integration tests use real FS
beforeEach(async () => {
  _httpRequestOverride = null;
  _netSocketPortOpen = null;
  _fsReadFileOverride = null;
  const actual = await vi.importActual<typeof import('./port-file.js')>('./port-file.js');
  mockReadPortFile.mockImplementation(actual.readPortFile);
  mockDeletePortFile.mockImplementation(actual.deletePortFile);
  mockWritePortFile.mockImplementation(actual.writePortFile);
  mockIsFresh.mockImplementation(actual.isFresh);
});

let tmpDir: string;

beforeEach(async () => {
  tmpDir = path.join(os.tmpdir(), crypto.randomUUID());
  await fsPromises.mkdir(tmpDir, { recursive: true });
});

afterEach(async () => {
  await fsPromises.rm(tmpDir, { recursive: true, force: true });
});

// ─────────────────────────────────────────────────────────────────────────────
// Mocked unit tests — no real I/O, fake timers
// ─────────────────────────────────────────────────────────────────────────────

describe('takeover — mocked (unit)', () => {
  beforeEach(() => {
    // Override port-file with pure mocks
    mockReadPortFile.mockResolvedValue(null);
    mockDeletePortFile.mockResolvedValue(undefined);
    mockIsFresh.mockResolvedValue(true);
    // Fake JS timers — NOT setImmediate (keep real so Promise.resolve().then() microtasks
    // can fire between timer advancements in advanceTimersByTimeAsync)
    vi.useFakeTimers({ toFake: ['setTimeout', 'setInterval', 'clearTimeout', 'clearInterval', 'Date'] });
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
    _httpRequestOverride = null;
    _netSocketPortOpen = null;
    _fsReadFileOverride = null;
  });

  it('TM-01: no port file → returns immediately, no kill, no fetch', async () => {
    mockReadPortFile.mockResolvedValue(null);
    const killSpy = vi.spyOn(process, 'kill').mockImplementation(() => true);
    _httpRequestOverride = makeHttpRequestMock();
    _netSocketPortOpen = () => false;

    await takeoverIfRunning(tmpDir, {});

    expect(killSpy).not.toHaveBeenCalled();
    expect(mockDeletePortFile).not.toHaveBeenCalled();
  });

  it('TM-02: port file with own PID → deletes file, no kill sent', async () => {
    mockReadPortFile.mockResolvedValue({
      sdkVersion: 1, port: 47823, pid: process.pid, startedAt: new Date().toISOString(),
    });
    mockIsFresh.mockResolvedValue(true);
    // Mock health_token read so no real I/O
    _fsReadFileOverride = async (_p, _enc) => 'test-token';
    const killSpy = vi.spyOn(process, 'kill').mockImplementation(() => true);
    // Port closes immediately
    _netSocketPortOpen = () => false;
    _httpRequestOverride = makeHttpRequestMock({
      rejectWith: Object.assign(new Error('ECONNREFUSED'), { code: 'ECONNREFUSED' }),
    });

    const p = takeoverIfRunning(tmpDir, {});
    await vi.advanceTimersByTimeAsync(500);
    await p;

    const nonZeroKills = killSpy.mock.calls.filter(([, sig]) => sig !== 0);
    expect(nonZeroKills).toHaveLength(0);
    expect(mockDeletePortFile).toHaveBeenCalledWith(tmpDir);
  });

  it('TM-03: port file with dead PID (ESRCH from kill(pid,0)) → deletePortFile called, no /quit', async () => {
    mockReadPortFile.mockResolvedValue({
      sdkVersion: 1, port: 47823, pid: 99999, startedAt: new Date().toISOString(),
    });
    const killSpy = vi.spyOn(process, 'kill').mockImplementation((_, sig) => {
      if (sig === 0) throw Object.assign(new Error('ESRCH'), { code: 'ESRCH' });
      return true;
    });
    let httpCallCount = 0;
    _httpRequestOverride = (...args) => { httpCallCount++; return makeHttpRequestMock()(...args); };

    await takeoverIfRunning(tmpDir, {});

    expect(httpCallCount).toBe(0);
    expect(mockDeletePortFile).toHaveBeenCalledWith(tmpDir);
  });

  it('TM-04: port file with stale mtime (isFresh returns false) → deletePortFile called, no /quit', async () => {
    mockReadPortFile.mockResolvedValue({
      sdkVersion: 1, port: 47823, pid: 99999, startedAt: new Date().toISOString(),
    });
    vi.spyOn(process, 'kill').mockImplementation(() => true);
    mockIsFresh.mockResolvedValue(false);
    let httpCallCount = 0;
    _httpRequestOverride = (...args) => { httpCallCount++; return makeHttpRequestMock()(...args); };

    await takeoverIfRunning(tmpDir, {});

    expect(httpCallCount).toBe(0);
    expect(mockDeletePortFile).toHaveBeenCalledWith(tmpDir);
  });

  it('TM-05: alive daemon, POST /quit succeeds, process dies gracefully → onTakeover called, deletePortFile called, no SIGTERM', async () => {
    mockReadPortFile.mockResolvedValue({
      sdkVersion: 1, port: 47823, pid: 12345, startedAt: new Date().toISOString(),
    });
    mockIsFresh.mockResolvedValue(true);
    // Mock health_token read: no real I/O (incompatible with fake timers)
    _fsReadFileOverride = async (_p, _enc) => 'test-token';

    const killSpy = vi.spyOn(process, 'kill').mockImplementation(() => true);
    _httpRequestOverride = makeHttpRequestMock(); // /quit succeeds

    let portPollCount = 0;
    // Port: open on first poll, closed on second
    _netSocketPortOpen = () => { portPollCount++; return portPollCount <= 1; };

    const onTakeover = vi.fn();
    const p = takeoverIfRunning(tmpDir, { onTakeover });
    await vi.advanceTimersByTimeAsync(3_200);
    await p;

    expect(onTakeover).toHaveBeenCalledWith(12345);
    expect(mockDeletePortFile).toHaveBeenCalled();
    const sigtermCalls = killSpy.mock.calls.filter(([, sig]) => sig === 'SIGTERM' || sig === 15);
    expect(sigtermCalls).toHaveLength(0);
  });

  it('TM-06: alive daemon, POST /quit fails (ECONNREFUSED) → SIGTERM sent', async () => {
    mockReadPortFile.mockResolvedValue({
      sdkVersion: 1, port: 47823, pid: 12345, startedAt: new Date().toISOString(),
    });
    mockIsFresh.mockResolvedValue(true);
    _fsReadFileOverride = async (_p, _enc) => 'test-token';

    const sigtermPids: number[] = [];
    const killSpy = vi.spyOn(process, 'kill').mockImplementation((pid, sig) => {
      if (sig === 0) {
        if (sigtermPids.length > 0) throw Object.assign(new Error('ESRCH'), { code: 'ESRCH' });
        return true;
      }
      if (sig === 'SIGTERM' || sig === 15) { sigtermPids.push(Number(pid)); return true; }
      return true;
    });

    _httpRequestOverride = makeHttpRequestMock({
      rejectWith: Object.assign(new Error('connect ECONNREFUSED'), { code: 'ECONNREFUSED' }),
    });
    // Port stays open until SIGTERM
    _netSocketPortOpen = () => sigtermPids.length === 0;

    const p = takeoverIfRunning(tmpDir, {});
    await vi.advanceTimersByTimeAsync(3_200);
    await p;

    expect(sigtermPids).toContain(12345);
  });

  it('TM-07: alive daemon, /quit succeeds but process never dies → SIGTERM sent', async () => {
    mockReadPortFile.mockResolvedValue({
      sdkVersion: 1, port: 47823, pid: 12345, startedAt: new Date().toISOString(),
    });
    mockIsFresh.mockResolvedValue(true);
    _fsReadFileOverride = async (_p, _enc) => 'test-token';

    const sigtermPids: number[] = [];
    const killSpy = vi.spyOn(process, 'kill').mockImplementation((pid, sig) => {
      if (sig === 0) {
        if (sigtermPids.length > 0) throw Object.assign(new Error('ESRCH'), { code: 'ESRCH' });
        return true;
      }
      if (sig === 'SIGTERM' || sig === 15) { sigtermPids.push(Number(pid)); return true; }
      return true;
    });

    _httpRequestOverride = makeHttpRequestMock(); // /quit succeeds
    // Port stays open even after /quit — process stuck
    _netSocketPortOpen = () => sigtermPids.length === 0;

    const p = takeoverIfRunning(tmpDir, {});
    await vi.advanceTimersByTimeAsync(7_000);
    await p;

    expect(sigtermPids).toContain(12345);
  });

  it('TM-08: health_token missing → /quit attempted with empty bearer, then SIGTERM sent', async () => {
    mockReadPortFile.mockResolvedValue({
      sdkVersion: 1, port: 47823, pid: 12345, startedAt: new Date().toISOString(),
    });
    mockIsFresh.mockResolvedValue(true);
    // Simulate missing health_token: readFile throws ENOENT
    _fsReadFileOverride = async (_p, _enc) => {
      throw Object.assign(new Error('ENOENT: no such file'), { code: 'ENOENT' });
    };

    const sigtermPids: number[] = [];
    const killSpy = vi.spyOn(process, 'kill').mockImplementation((pid, sig) => {
      if (sig === 0) {
        if (sigtermPids.length > 0) throw Object.assign(new Error('ESRCH'), { code: 'ESRCH' });
        return true;
      }
      if (sig === 'SIGTERM' || sig === 15) { sigtermPids.push(Number(pid)); return true; }
      return true;
    });

    // /quit fails (empty bearer token rejected) → port stays open → SIGTERM
    _httpRequestOverride = makeHttpRequestMock({
      rejectWith: Object.assign(new Error('ECONNREFUSED'), { code: 'ECONNREFUSED' }),
    });
    _netSocketPortOpen = () => sigtermPids.length === 0;

    const p = takeoverIfRunning(tmpDir, {});
    await vi.advanceTimersByTimeAsync(3_200);
    await p;

    expect(sigtermPids).toContain(12345);
  });

  it('TM-09: process survives SIGTERM entirely → onTakeoverFailed called, DaemonTakeoverError thrown, deletePortFile NOT called', async () => {
    mockReadPortFile.mockResolvedValue({
      sdkVersion: 1, port: 47823, pid: 12345, startedAt: new Date().toISOString(),
    });
    mockIsFresh.mockResolvedValue(true);
    _fsReadFileOverride = async (_p, _enc) => 'test-token';

    // Always alive — never throws ESRCH
    vi.spyOn(process, 'kill').mockImplementation(() => true);

    _httpRequestOverride = makeHttpRequestMock({
      rejectWith: Object.assign(new Error('ECONNREFUSED'), { code: 'ECONNREFUSED' }),
    });
    // Port always open
    _netSocketPortOpen = () => true;

    const { DaemonTakeoverError } = await import('./types.js');
    const onTakeoverFailed = vi.fn();

    let caught: unknown;
    const p = takeoverIfRunning(tmpDir, { onTakeoverFailed }).catch(e => { caught = e; });
    await vi.advanceTimersByTimeAsync(7_000);
    await p;

    expect(caught).toBeInstanceOf(DaemonTakeoverError);
    expect(onTakeoverFailed).toHaveBeenCalledWith(12345);
    expect(mockDeletePortFile).not.toHaveBeenCalled();
  });
});

// ─────────────────────────────────────────────────────────────────────────────
// Integration tests (real port-file, real http, real net)
// ─────────────────────────────────────────────────────────────────────────────

describe('takeover', () => {
  it('T13: port file with dead PID (ESRCH) → file deleted, no error', async () => {
    // Write a port file with a PID that does not exist
    // Use a very high PID that is unlikely to be running
    await writePortFile(tmpDir, 12345, 9999999);
    await takeoverIfRunning(tmpDir, {});
    const data = await readPortFile(tmpDir);
    expect(data).toBeNull();
  });

  it('T14: stale mtime (> 60s) → file deleted, no error', async () => {
    await writePortFile(tmpDir, 12345, process.pid);
    // Age the file by 70 seconds
    const oldTime = new Date(Date.now() - 70_000);
    const filePath = path.join(tmpDir, 'config.port');
    await fsPromises.utimes(filePath, oldTime, oldTime);
    await takeoverIfRunning(tmpDir, {});
    const data = await readPortFile(tmpDir);
    expect(data).toBeNull();
  });

  it('T15: alive daemon → evicted via POST /quit, onTakeover called', async () => {
    // Write child script to a temp file (avoids issues with -e and argv on Windows)
    const scriptPath = path.join(tmpDir, 'child-daemon.cjs');
    await fsPromises.writeFile(scriptPath, `
      const http = require('http');
      const fs = require('fs');
      const path = require('path');
      const configDir = process.argv[2];
      const server = http.createServer((req, res) => {
        if (req.method === 'POST' && req.url === '/quit') {
          res.writeHead(200, { 'Content-Type': 'application/json' });
          res.end(JSON.stringify({ ok: true }));
          server.close(() => process.exit(0));
        } else {
          res.writeHead(404);
          res.end();
        }
      });
      server.listen(0, '127.0.0.1', () => {
        const port = server.address().port;
        const data = { sdkVersion: 1, port, pid: process.pid, startedAt: new Date().toISOString() };
        const filePath = path.join(configDir, 'config.port');
        fs.writeFileSync(filePath + '.tmp', JSON.stringify(data));
        fs.renameSync(filePath + '.tmp', filePath);
        // Signal ready via stdout
        process.stdout.write('READY:' + port + '\\n');
      });
    `);

    const { spawn } = await import('child_process');
    const child = spawn(process.execPath, [scriptPath, tmpDir], {
      stdio: ['ignore', 'pipe', 'inherit'],
    });

    // Wait for child to write port file
    await new Promise<void>((resolve, reject) => {
      let buf = '';
      child.stdout!.on('data', (d: Buffer) => {
        buf += d.toString();
        if (buf.includes('READY:')) resolve();
      });
      child.on('error', reject);
      setTimeout(() => reject(new Error('child timeout')), 5000);
    });

    // Write health_token
    await fsPromises.writeFile(path.join(tmpDir, 'health_token'), 'test-token-abc', { mode: 0o600 });

    const onTakeover = vi.fn();
    await takeoverIfRunning(tmpDir, { onTakeover });

    expect(onTakeover).toHaveBeenCalled();

    // Wait for child to exit
    await new Promise<void>((resolve) => child.on('exit', () => resolve()));
  });
});
