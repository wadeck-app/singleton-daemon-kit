export type CommandHandler<R = void> = (payload?: unknown) => R | Promise<R>;
export type CommandMap = Record<string, CommandHandler<unknown>>;
export type CommandName<T extends CommandMap> = keyof T & string;
export type CommandResult<T extends CommandMap, K extends keyof T> =
  ReturnType<T[K]> extends Promise<infer R> ? R : ReturnType<T[K]>;

export interface DaemonOptions<T extends CommandMap> {
  configDir: string;
  commands: T;
  port?: number;
  idleTimeout?: number | null;
  drainTimeout?: number;
  health?: () => HealthStatus;
  versionExtra?: () => Record<string, unknown>;
  hooks?: DaemonHooks;
}

export interface DaemonHooks {
  onStart?: (port: number) => void;
  onShutdown?: (reason: ShutdownReason) => void;
  onCommand?: (name: string, durationMs: number) => void;
  onCommandError?: (name: string, error: Error) => void;
  onTakeover?: (evictedPid: number) => void;
  onTakeoverFailed?: (pid: number) => void;
}

// 'update' is used by consumers that implement auto-update (e.g. wdrive).
// See wdrive inspiration: driver/src/updater/ (Updater class) + driver/src/index.ts
// (applyUpdate command handler calls handle.stop('update') then re-spawns the process).
export type ShutdownReason = 'idle' | 'signal' | 'command' | 'error' | 'update';

export interface HealthStatus {
  status: 'ok' | 'degraded' | 'offline';
  [key: string]: unknown;
}

export interface DaemonHandle {
  stop(reason?: ShutdownReason): Promise<void>;
  readonly port: number;
}

export interface DaemonClient<T extends CommandMap> {
  send<K extends CommandName<T>>(command: K, payload?: unknown): Promise<CommandResult<T, K>>;
  isRunning(): Promise<boolean>;
  version(): Promise<{ version: string; pid: number; config_dir: string; sdkVersion: number; [key: string]: unknown }>;
}

export interface TestDaemonHandle<T extends CommandMap> extends DaemonHandle {
  client: DaemonClient<T>;
  configDir: string;
  [Symbol.asyncDispose](): Promise<void>;
}

export class DaemonNotRunningError extends Error {
  constructor(message: string) { super(message); this.name = 'DaemonNotRunningError'; }
}
export class DaemonTakeoverError extends Error {
  constructor(message: string) { super(message); this.name = 'DaemonTakeoverError'; }
}
export class DaemonVersionError extends Error {
  constructor(message: string) { super(message); this.name = 'DaemonVersionError'; }
}
export class DaemonPortExhaustedError extends Error {
  constructor(message: string) { super(message); this.name = 'DaemonPortExhaustedError'; }
}
