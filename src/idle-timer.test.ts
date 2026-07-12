import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createIdleTimer } from './idle-timer.js';

describe('idle-timer', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('T8: idleTimeout fires with no commands → onIdle called after timeout', async () => {
    const onIdle = vi.fn();
    const timer = createIdleTimer(1000, 500, onIdle);

    vi.advanceTimersByTime(1001);
    await Promise.resolve(); // flush microtasks
    expect(onIdle).toHaveBeenCalledOnce();
    timer.dispose();
  });

  it('T9: command in-flight when timer fires → waits for commandFinished then calls onIdle', async () => {
    const onIdle = vi.fn();
    const timer = createIdleTimer(1000, 5000, onIdle);

    timer.commandStarted();
    vi.advanceTimersByTime(1001);
    await Promise.resolve();
    // onIdle should not be called yet because command is in-flight
    expect(onIdle).not.toHaveBeenCalled();

    timer.commandFinished();
    await Promise.resolve();
    await Promise.resolve();
    expect(onIdle).toHaveBeenCalledOnce();
    timer.dispose();
  });

  it('T10: drainTimeout exceeded → onIdle called anyway', async () => {
    const onIdle = vi.fn();
    const timer = createIdleTimer(1000, 500, onIdle);

    timer.commandStarted();
    vi.advanceTimersByTime(1001);
    await Promise.resolve();
    expect(onIdle).not.toHaveBeenCalled();

    // Exceed drain timeout without finishing command
    vi.advanceTimersByTime(501);
    await Promise.resolve();
    await Promise.resolve();
    expect(onIdle).toHaveBeenCalledOnce();
    timer.dispose();
  });

  it('null idleTimeout → onIdle never called', async () => {
    const onIdle = vi.fn();
    const timer = createIdleTimer(null, 500, onIdle);
    vi.advanceTimersByTime(100_000);
    await Promise.resolve();
    expect(onIdle).not.toHaveBeenCalled();
    timer.dispose();
  });
});
