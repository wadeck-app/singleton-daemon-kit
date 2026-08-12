export function isProcessAlive(pid: number): boolean {
  try {
    process.kill(pid, 0);
    return true;
  } catch (err: unknown) {
    const e = err as NodeJS.ErrnoException;
    if (e.code === 'ESRCH') return false;
    // EPERM: process exists but we lack permission to signal it
    if (e.code === 'EPERM') return true;
    return false;
  }
}
