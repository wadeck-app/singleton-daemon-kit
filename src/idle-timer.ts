export interface IdleTimer {
  reset(): void;
  commandStarted(): void;
  commandFinished(): void;
  dispose(): void;
}

export function createIdleTimer(
  idleTimeoutMs: number | null,
  drainTimeoutMs: number,
  onIdle: () => void
): IdleTimer {
  if (idleTimeoutMs === null) {
    return {
      reset: () => {},
      commandStarted: () => {},
      commandFinished: () => {},
      dispose: () => {},
    };
  }

  let inFlight = 0;
  let idleTimer: ReturnType<typeof setTimeout> | null = null;
  let drainTimer: ReturnType<typeof setTimeout> | null = null;
  let disposed = false;

  function clearTimers() {
    if (idleTimer !== null) { clearTimeout(idleTimer); idleTimer = null; }
    if (drainTimer !== null) { clearTimeout(drainTimer); drainTimer = null; }
  }

  function scheduleIdle() {
    clearTimers();
    if (disposed) return;
    idleTimer = setTimeout(() => {
      idleTimer = null;
      if (inFlight > 0) {
        // Wait for drain timeout
        drainTimer = setTimeout(() => {
          drainTimer = null;
          if (!disposed) onIdle();
        }, drainTimeoutMs);
      } else {
        if (!disposed) onIdle();
      }
    }, idleTimeoutMs!);
  }

  scheduleIdle();

  return {
    reset() {
      if (!disposed) scheduleIdle();
    },
    commandStarted() {
      inFlight++;
    },
    commandFinished() {
      if (inFlight > 0) inFlight--;
      // If drain timer is running and inFlight reaches 0, fire onIdle
      if (inFlight === 0 && drainTimer !== null) {
        clearTimeout(drainTimer);
        drainTimer = null;
        if (!disposed) {
          Promise.resolve().then(() => { if (!disposed) onIdle(); });
        }
      }
    },
    dispose() {
      disposed = true;
      clearTimers();
    },
  };
}
