import type { ProfitInputs, ProfitScenario } from './types';
export interface ProfitSaveTask {
  id: string;
  inputs: ProfitInputs;
  dataRevision: number;
  monthlyId: string | null;
  monthlyRevision: number | null;
}
type Entry = {
  task: ProfitSaveTask;
  fingerprint: string;
  key: string;
  timer?: ReturnType<typeof setTimeout>;
  ready: boolean;
  running: boolean;
  failed: boolean;
  result?: ProfitScenario;
};
type Callbacks = {
  state: (task: ProfitSaveTask, state: 'queued' | 'saving' | 'failed', error?: string) => void;
  saved: (task: ProfitSaveTask, scenario: ProfitScenario) => void;
};

// One writer per ASIN. New edits wait behind an in-flight write; retries reuse
// the same idempotency key, including after a lost response.
export class ProfitSaveQueue {
  private entries = new Map<string, Entry>();
  private active = 0;
  constructor(
    private send: (task: ProfitSaveTask, key: string) => Promise<ProfitScenario>,
    private callbacks: Callbacks,
    private delay = 800
  ) {}
  enqueue(task: ProfitSaveTask) {
    const fingerprint = JSON.stringify([
      task.inputs,
      task.dataRevision,
      task.monthlyId,
      task.monthlyRevision
    ]);
    const old = this.entries.get(task.id);
    if (old?.fingerprint === fingerprint) {
      old.task = task;
      if (old.failed) this.callbacks.state(task, 'failed', '自动保存未成功，请重试');
      if (old.result) this.callbacks.saved(task, old.result);
      return;
    }
    if (old?.timer) clearTimeout(old.timer);
    const entry: Entry = {
      task,
      fingerprint,
      key: crypto.randomUUID(),
      ready: false,
      running: old?.running || false,
      failed: false
    };
    this.entries.set(task.id, entry);
    this.callbacks.state(task, 'queued');
    entry.timer = setTimeout(() => {
      entry.ready = true;
      this.pump();
    }, this.delay);
  }
  retry(id: string) {
    const entry = this.entries.get(id);
    if (!entry || entry.running) return;
    entry.failed = false;
    entry.ready = true;
    this.pump();
  }
  hasPending() {
    return [...this.entries.values()].some((entry) => !entry.result);
  }
  flush(id?: string) {
    for (const entry of this.entries.values()) {
      if (id && entry.task.id !== id) continue;
      if (entry.timer) clearTimeout(entry.timer);
      if (!entry.failed && !entry.result) entry.ready = true;
    }
    this.pump();
  }
  private pump() {
    for (const [id, entry] of this.entries) {
      if (this.active >= 3) break;
      if (!entry.ready || entry.running || entry.failed || entry.result) continue;
      entry.running = true;
      entry.ready = false;
      this.active++;
      this.callbacks.state(entry.task, 'saving');
      void this.send(entry.task, entry.key)
        .then((scenario) => {
          entry.result = scenario;
          this.callbacks.saved(entry.task, scenario);
        })
        .catch((error: unknown) => {
          entry.failed = true;
          this.callbacks.state(
            entry.task,
            'failed',
            error instanceof Error ? error.message : '自动保存失败，请重试'
          );
        })
        .finally(() => {
          this.active--;
          const latest = this.entries.get(id);
          if (latest) latest.running = false;
          this.pump();
        });
    }
  }
}
