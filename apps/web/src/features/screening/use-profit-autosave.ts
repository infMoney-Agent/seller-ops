'use client';
import { useEffect, useMemo, type Dispatch, type SetStateAction } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { useSellerSession } from '@/features/auth/session';
import { ProfitSaveQueue, type ProfitSaveTask } from './profit-save-queue';
import { validateProfitDraft } from './profit-validation';
import { request } from './service';
import type { ProfitRowDraft } from './profit-table';
import type { ProfitScenario } from './types';

class AutosaveLifecycle {
  private active = true;
  constructor(readonly workspaceId: ReturnType<typeof useSellerSession>['workspaceId']) {}
  isActive() {
    return this.active;
  }
  resume() {
    this.active = true;
  }
  pause() {
    this.active = false;
  }
}

export function useProfitAutosave(
  drafts: Record<string, ProfitRowDraft>,
  setDrafts: Dispatch<SetStateAction<Record<string, ProfitRowDraft>>>
) {
  const client = useQueryClient(),
    session = useSellerSession(),
    lifecycle = useMemo(() => new AutosaveLifecycle(session.workspaceId), [session.workspaceId]);
  const queue = useMemo(
    () =>
      new ProfitSaveQueue(
        async (task, key) => {
          const error = Object.values(validateProfitDraft(task.inputs))[0];
          if (error) throw new Error(error);
          return request<ProfitScenario>(
            '/products/' + task.id + '/profit-scenarios',
            'POST',
            {
              expected_data_revision: task.dataRevision,
              monthly_id: task.monthlyId,
              expected_monthly_revision: task.monthlyRevision,
              inputs: task.inputs
            },
            undefined,
            'auto',
            { idempotencyKey: key, keepalive: true, workspaceId: String(session.workspaceId ?? '') }
          );
        },
        {
          state(task, state, error = '') {
            if (lifecycle.isActive())
              setDrafts((old) => {
                const row = old[task.id];
                return row?.inputs === task.inputs
                  ? { ...old, [task.id]: { ...row, saveState: state, error } }
                  : old;
              });
          },
          saved(task, scenario) {
            void client.invalidateQueries({
              queryKey: ['screening', session.workspaceId],
              predicate: (q) =>
                typeof q.queryKey[2] === 'string' &&
                (q.queryKey[2].startsWith('/profit/products') ||
                  q.queryKey[2].endsWith('/profit-scenarios')),
              refetchType: lifecycle.isActive() ? 'none' : 'active'
            });
            if (!lifecycle.isActive()) return;
            setDrafts((old) => {
              const row = old[task.id];
              return row?.inputs === task.inputs
                ? {
                    ...old,
                    [task.id]: {
                      ...row,
                      inputs: scenario.inputs,
                      output: scenario.output,
                      dirty: false,
                      saveState: 'saved',
                      error: ''
                    }
                  }
                : old;
            });
          }
        }
      ),
    [client, lifecycle, session.workspaceId, setDrafts]
  );
  useEffect(() => {
    for (const [id, row] of Object.entries(drafts))
      if (row.dirty)
        queue.enqueue({
          id,
          inputs: row.inputs,
          dataRevision: row.dataRevision,
          monthlyId: row.monthlyId,
          monthlyRevision: row.monthlyRevision
        } satisfies ProfitSaveTask);
  }, [drafts, queue]);
  useEffect(() => {
    lifecycle.resume();
    const flush = () => queue.flush();
    const hide = () => {
      if (document.visibilityState === 'hidden') queue.flush();
    };
    document.addEventListener('visibilitychange', hide);
    const guard = (event: BeforeUnloadEvent) => {
      if (queue.hasPending()) {
        queue.flush();
        event.preventDefault();
        event.returnValue = '';
      }
    };
    window.addEventListener('pagehide', flush);
    window.addEventListener('beforeunload', guard);
    return () => {
      document.removeEventListener('visibilitychange', hide);
      lifecycle.pause();
      queue.flush();
      window.removeEventListener('pagehide', flush);
      window.removeEventListener('beforeunload', guard);
    };
  }, [queue, lifecycle]);
  return queue;
}
