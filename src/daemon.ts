import { takeoverIfRunning } from './takeover.js';
import { writePortFile, startHeartbeat, deletePortFile } from './port-file.js';
import { startHealthServer } from './health-server.js';
import { createIdleTimer } from './idle-timer.js';
import { acquireStartupLock } from './startup-lock.js';
import { type DaemonOptions, type DaemonHandle, type ShutdownReason, type CommandMap } from './types.js';

export async function createDaemon<T extends CommandMap>(options: DaemonOptions<T>): Promise<DaemonHandle> {
  const {
    configDir,
    commands,
    port,
    idleTimeout = null,
    drainTimeout = 5000,
    health,
    appVersion,
    versionExtra,
    hooks = {},
  } = options;

  // Acquire startup lock before takeoverIfRunning to prevent concurrent race:
  // two instances starting simultaneously would both read the same port file,
  // both evict the existing daemon, and both start their own health servers.
  const releaseLock = await acquireStartupLock(configDir);

  let serverHandle: Awaited<ReturnType<typeof startHealthServer>>;
  let actualPort: number;

  try {
    // Step 1: takeover if running
    await takeoverIfRunning(configDir, hooks);

    // Step 2: start health server (writes health_token)
    serverHandle = await startHealthServer({
      configDir,
      commands,
      port,
      health,
      appVersion,
      versionExtra,
      hooks,
      onQuit: () => handle.stop('command'),
    });

    actualPort = serverHandle.port;

    // Step 3: write port file — lock is released only after this so the next
    // waiter sees a fresh, valid port file when it acquires the lock.
    await writePortFile(configDir, actualPort, process.pid);
  } finally {
    await releaseLock();
  }

  // Step 4: start heartbeat
  const stopHeartbeat = startHeartbeat(configDir);

  // Step 5: start idle timer
  const idleTimer = createIdleTimer(
    idleTimeout ?? null,
    drainTimeout,
    () => { void handle.stop('idle'); }
  );

  let stopped = false;

  // Step 6: SIGTERM/SIGINT handlers
  const onSignal = () => { void handle.stop('signal'); };
  process.once('SIGTERM', onSignal);
  process.once('SIGINT', onSignal);

  const handle: DaemonHandle = {
    get port() { return actualPort; },
    async stop(reason: ShutdownReason = 'command'): Promise<void> {
      if (stopped) return;
      stopped = true;

      process.removeListener('SIGTERM', onSignal);
      process.removeListener('SIGINT', onSignal);

      idleTimer.dispose();
      stopHeartbeat();
      await deletePortFile(configDir);
      await serverHandle.close();
      hooks.onShutdown?.(reason);
    },
  };

  // Step 7: call onStart hook
  hooks.onStart?.(actualPort);

  return handle;
}
