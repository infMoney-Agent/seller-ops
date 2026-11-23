import assert from 'node:assert/strict';
import { mkdtemp, readFile, writeFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { pathToFileURL } from 'node:url';
import ts from 'typescript';

const directory = await mkdtemp(join(tmpdir(), 'profit-queue-test-'));
try {
  const source = await readFile(
    new URL('../src/features/screening/profit-save-queue.ts', import.meta.url),
    'utf8'
  );
  const modulePath = join(directory, 'queue.mjs');
  await writeFile(
    modulePath,
    ts.transpileModule(source, {
      compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 }
    }).outputText
  );
  const { ProfitSaveQueue } = await import(pathToFileURL(modulePath).href);
  const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
  const until = async (predicate) => {
    for (let i = 0; i < 200; i++) {
      if (predicate()) return;
      await sleep(10);
    }
    assert.fail('queue did not settle');
  };
  const task = (id, value) => ({
    id,
    inputs: { name: 'test', purchase_cny: value },
    dataRevision: 1,
    monthlyId: null,
    monthlyRevision: null
  });
  const result = (current) => ({ inputs: current.inputs, output: { state: 'incomplete' } });
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
  console.log(
    'PASS: debounce, deduplication, serialized edits, retained failures, stable retry key, concurrency limit and navigation flush'
  );
} finally {
  await rm(directory, { recursive: true, force: true });
}
