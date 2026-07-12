import * as fs from 'fs/promises';
import * as path from 'path';

const PORT_FILE = 'config.port';
const FRESHNESS_MS = 60_000;
const HEARTBEAT_INTERVAL_MS = 30_000;

export interface PortFileData {
  sdkVersion: number;
  port: number;
  pid: number;
  startedAt: string;
}

export async function writePortFile(configDir: string, port: number, pid: number): Promise<void> {
  const data: PortFileData = {
    sdkVersion: 1,
    port,
    pid,
    startedAt: new Date().toISOString(),
  };
  const filePath = path.join(configDir, PORT_FILE);
  const tmpPath = filePath + '.tmp';
  await fs.writeFile(tmpPath, JSON.stringify(data), 'utf8');
  await fs.rename(tmpPath, filePath);
}

export async function readPortFile(configDir: string): Promise<PortFileData | null> {
  const filePath = path.join(configDir, PORT_FILE);
  try {
    const content = await fs.readFile(filePath, 'utf8');
    const data = JSON.parse(content) as PortFileData;
    if (typeof data.port !== 'number' || typeof data.pid !== 'number') return null;
    return data;
  } catch {
    return null;
  }
}

export function startHeartbeat(configDir: string): () => void {
  const filePath = path.join(configDir, PORT_FILE);
  const interval = setInterval(async () => {
    const now = new Date();
    try {
      await fs.utimes(filePath, now, now);
    } catch {
      // File may have been deleted during shutdown
    }
  }, HEARTBEAT_INTERVAL_MS);
  interval.unref?.();
  return () => clearInterval(interval);
}

export async function isFresh(configDir: string): Promise<boolean> {
  const filePath = path.join(configDir, PORT_FILE);
  try {
    const stat = await fs.stat(filePath);
    return (Date.now() - stat.mtimeMs) < FRESHNESS_MS;
  } catch {
    return false;
  }
}

export async function deletePortFile(configDir: string): Promise<void> {
  const filePath = path.join(configDir, PORT_FILE);
  try {
    await fs.unlink(filePath);
  } catch {
    // Ignore if not found
  }
}
