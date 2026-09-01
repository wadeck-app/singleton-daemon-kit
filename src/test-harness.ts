import * as os from 'node:os';
import * as crypto from 'node:crypto';
import * as fs from 'node:fs/promises';
import * as path from 'node:path';
import { createDaemon } from './daemon.js';
import { createDaemonClient } from './client.js';
import {
  type TestDaemonHandle,
  type DaemonOptions,
  type CommandMap,
} from './types.js';

export async function createTestDaemon<T extends CommandMap>(
  options: Omit<DaemonOptions<T>, 'configDir'>
): Promise<TestDaemonHandle<T>> {
  const tmpDir = path.join(os.tmpdir(), crypto.randomUUID());
  await fs.mkdir(tmpDir, { recursive: true });

  const port = options.port ?? 0;

  const daemon = await createDaemon({
    ...options,
    configDir: tmpDir,
    port,
  });

  const client = createDaemonClient<T>({ configDir: tmpDir, commands: options.commands });

  const handle: TestDaemonHandle<T> = {
    client,
    configDir: tmpDir,
    get port() { return daemon.port; },
    async stop(reason?) {
      return daemon.stop(reason);
    },
    async [Symbol.asyncDispose]() {
      await daemon.stop('command');
      await fs.rm(tmpDir, { recursive: true, force: true });
    },
  };

  return handle;
}
