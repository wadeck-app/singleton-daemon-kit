import * as http from 'node:http';

/**
 * Minimal HTTP GET helper used by both client.ts (liveness check) and
 * health-server.ts / startup-lock.ts (port-exhaustion guard).
 * Timeouts after `timeoutMs` milliseconds (default 5 000).
 */
export function httpGet(
  port: number,
  urlPath: string,
  timeoutMs = 5_000,
): Promise<{ status: number; body: Record<string, unknown> }> {
  return new Promise((resolve, reject) => {
    const req = http.request(
      { hostname: '127.0.0.1', port, method: 'GET', path: urlPath },
      (res) => {
        let data = '';
        res.on('data', (chunk) => { data += chunk; });
        res.on('end', () => {
          try {
            resolve({ status: res.statusCode ?? 0, body: JSON.parse(data) as Record<string, unknown> });
          } catch {
            reject(new Error(`Invalid JSON response: ${data}`));
          }
        });
      },
    );
    req.setTimeout(timeoutMs, () => {
      req.destroy(new Error(`HTTP timeout after ${timeoutMs}ms`));
    });
    req.on('error', reject);
    req.end();
  });
}

/**
 * Returns true if GET /version on the given port responds with HTTP 200
 * within timeoutMs milliseconds.
 * Used as an MSYS2 fallback when isProcessAlive() returns a false-negative.
 *
 * Uses Promise.race so the deadline is enforced by an external timer — more
 * reliable than req.setTimeout which can be reset by TCP ACK activity.
 */
export function isHttpReachable(port: number, timeoutMs = 2_000): Promise<boolean> {
  const deadline = new Promise<false>(resolve => setTimeout(() => resolve(false), timeoutMs));
  const check = httpGet(port, '/version').then(r => r.status === 200).catch(() => false);
  return Promise.race([deadline, check]);
}
