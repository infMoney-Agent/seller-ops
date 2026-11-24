import assert from 'node:assert/strict';
import { mkdtemp, readFile, writeFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';
import ts from 'typescript';

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
const task = (id, value) => ({
  id,
  inputs: { name: 'test', purchase_cny: value },
  dataRevision: 1,
  monthlyId: null,
  monthlyRevision: null
});
const result = (current) => ({ inputs: current.inputs, output: { state: 'incomplete' } });

const cryptoDescriptor = Object.getOwnPropertyDescriptor(globalThis, 'crypto');
const nativeCrypto = globalThis.crypto;
let randomReads = 0;
Object.defineProperty(globalThis, 'crypto', { configurable: true, value: { getRandomValues(bytes) { randomReads++; return nativeCrypto.getRandomValues(bytes); } } });
assert.equal(typeof globalThis.crypto.randomUUID, 'undefined');
const browserUUID = new URL('../node_modules/uuid/dist/esm-browser/index.js', import.meta.url).href;
const directory = await mkdtemp(join(tmpdir(), 'profit-queue-test-'));
try {
  const source = await readFile(
    new URL('../src/features/screening/profit-save-queue.ts', import.meta.url),
    'utf8'
  );
  const modulePath = join(directory, 'queue.mjs');
  await writeFile(
    modulePath,
    ts.transpileModule(source.replace("from 'uuid'", "from '" + browserUUID + "'"), {
      compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 }
    }).outputText
  );
  const { ProfitSaveQueue } = await import(pathToFileURL(modulePath).href);
  const until = async (predicate) => {
    for (let i = 0; i < 200; i++) {
      if (predicate()) return;
      await sleep(10);
    }
    assert.fail('queue did not settle');
  };
  const observer = { state() {}, saved() {} };
  let calls = [];
  let queue = new ProfitSaveQueue(
    async (current, key) => {
      calls.push([current, key]);
      return result(current);
    },
    observer,
    10
  );
  queue.enqueue(task('a', '1'));
  queue.enqueue(task('a', '2'));
  await until(() => !queue.hasPending());
  assert.equal(calls.length, 1);
  assert.equal(calls[0][0].inputs.purchase_cny, '2');
  queue.enqueue(task('a', '2'));
  await sleep(20);
  assert.equal(calls.length, 1);
  let release;
  const order = [];
  queue = new ProfitSaveQueue(
    async (current) => {
      order.push(current.inputs.purchase_cny);
      if (order.length === 1) await new Promise((resolve) => (release = resolve));
      return result(current);
    },
    observer,
    5
  );
  queue.enqueue(task('a', '1'));
  await until(() => order.length === 1);
  queue.enqueue(task('a', '2'));
  await sleep(20);
  assert.deepEqual(order, ['1']);
  release();
  await until(() => !queue.hasPending());
  assert.deepEqual(order, ['1', '2']);
  const keys = [];
  queue = new ProfitSaveQueue(
    async (current, key) => {
      keys.push(key);
      if (keys.length === 1) throw Error('lost response');
      return result(current);
    },
    observer,
    5
  );
  queue.enqueue(task('b', '3'));
  await until(() => keys.length === 1);
  await sleep(20);
  assert.equal(keys.length, 1);
  assert.equal(queue.hasPending(), true);
  queue.retry('b');
  await until(() => !queue.hasPending());
  assert.equal(keys.length, 2);
  assert.equal(keys[0], keys[1]);
  const failureStates = [];
  const failedTask = task('failed-row', '7');
  const failedQueue = new ProfitSaveQueue(async () => { throw Error('具体保存错误'); }, { state(_task, state, error) { if (state === 'failed') failureStates.push(error); }, saved() {} }, 5);
  failedQueue.enqueue(failedTask);
  await until(() => failureStates.length === 1);
  for (let i=0;i<100;i++) failedQueue.enqueue(failedTask);
  assert.deepEqual(failureStates, ['具体保存错误'], 'unchanged failed input must not trigger a render feedback loop');
  failedQueue.enqueue({ ...failedTask, inputs: { ...failedTask.inputs } });
  assert.deepEqual(failureStates, ['具体保存错误','具体保存错误'], 'cloned input must retain original failure details');
  process.stdout.write('PASS: failed autosave settles without re-render feedback\n');
  let active = 0,
    maximum = 0,
    completed = 0;
  queue = new ProfitSaveQueue(
    async (current) => {
      active++;
      maximum = Math.max(maximum, active);
      await sleep(10);
      active--;
      completed++;
      return result(current);
    },
    observer,
    10000
  );
  for (let i = 0; i < 10; i++) queue.enqueue(task(String(i), '1'));
  queue.flush();
  await until(() => !queue.hasPending());
  assert.equal(completed, 10);
  assert.equal(maximum, 3);
  process.stdout.write(
    'PASS: debounce, deduplication, serialized edits, retained failures, stable retry key, concurrency limit and navigation flush\n'
  );
  const serviceSource = await readFile(new URL('../src/features/screening/service.ts', import.meta.url), 'utf8');
  const servicePath = join(directory, 'service.mjs');
  await writeFile(servicePath, ts.transpileModule(serviceSource.replace("from 'uuid'", "from '" + browserUUID + "'"), { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } }).outputText);
  const originalFetch = globalThis.fetch, originalDocument = globalThis.document;
  let generatedKey;
  try {
    globalThis.document = { querySelector() { return null; } };
    globalThis.fetch = async (_url, options) => { generatedKey = options.headers['Idempotency-Key']; return Response.json({ saved: true }); };
    const { request } = await import(pathToFileURL(servicePath).href);
    assert.deepEqual(await request('/example', 'POST', { value: 1 }), { saved: true });
    assert.match(generatedKey, /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
    await request('/example', 'POST', {}, undefined, 'auto', { idempotencyKey: 'retry-original' });
    assert.equal(generatedKey, 'retry-original');
    process.stdout.write('PASS: public HTTP queue and API writes without crypto.randomUUID\n');
  } finally { globalThis.fetch = originalFetch; if (originalDocument === undefined) delete globalThis.document; else globalThis.document = originalDocument; }
} finally {
  assert.ok(randomReads > 0, 'HTTP fallback must use cryptographic random values');
  if (cryptoDescriptor) Object.defineProperty(globalThis, 'crypto', cryptoDescriptor);
  await rm(directory, { recursive: true, force: true });
}
