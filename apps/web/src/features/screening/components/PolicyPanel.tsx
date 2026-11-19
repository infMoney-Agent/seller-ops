'use client';
import { useState } from 'react';
import { useSellerSession } from '@/features/auth/session';
import { useScreeningQuery } from '../queries';
import { request } from '../service';
import type { JobCreated, Json, Policy } from '../types';
import { Badge, Button, DataBlock, Field, Input, type Action } from './shared';

export default function PolicyPanel({
  selected,
  act,
  watch,
  busy
}: {
  selected: string[];
  act: Action;
  watch: (id: string) => void;
  busy: boolean;
}) {
  const session = useSellerSession();
  const policies = useScreeningQuery<{ items: Policy[]; default_config: Record<string, Json> }>(
    '/policies'
  );
  const [name, setName] = useState('初筛规则');
  const [draft, setDraft] = useState('');
  const [test, setTest] = useState<unknown>(null);
  const current = policies.data?.items.find((p) => p.active);
  const config = (() => {
    try {
      return JSON.parse(
        draft || JSON.stringify(current?.config || policies.data?.default_config || {})
      ) as Record<string, Json>;
    } catch {
      return null;
    }
  })();
  function toggle(key: string, enabled: boolean) {
    if (!config) return;
    setDraft(
      JSON.stringify(
        { ...config, exclude: { ...(config.exclude as Record<string, Json>), [key]: enabled } },
        null,
        2
      )
    );
  }
  return (
    <div className='space-y-4'>
      <p className='text-muted-foreground text-sm'>
        规则启用后重新计算机器结果，不改变人工结论。公共规则不包含历史 ASIN 特例。
      </p>
      {policies.isError && <p role='alert'>{policies.error.message}</p>}
      <div className='flex flex-wrap gap-3'>
        {Object.entries((config?.exclude || {}) as Record<string, Json>).map(([key, value]) => (
          <label key={key} className='text-sm'>
            <input
              type='checkbox'
              aria-label={`排除${key}`}
              checked={Boolean(value)}
              disabled={session.role !== 'owner'}
              onChange={(e) => toggle(key, e.target.checked)}
            />{' '}
            排除{key}
          </label>
        ))}
      </div>
      {session.role === 'owner' && (
        <div className='space-y-3 rounded border p-4'>
          <Field label='规则版本名称'>
            <Input value={name} onChange={(e) => setName(e.target.value)} />
          </Field>
          <Field label='规则 JSON（开关、阈值、词表和高级表达式）'>
            <textarea
              aria-label='规则 JSON'
              className='min-h-72 rounded border p-3 font-mono text-xs'
              value={
                draft ||
                JSON.stringify(current?.config || policies.data?.default_config || {}, null, 2)
              }
              onChange={(e) => setDraft(e.target.value)}
            />
          </Field>
          <Button
            disabled={busy || !config}
            onClick={() =>
              void act(
                () => request('/policies', 'POST', { name, config }),
                '规则新版本已保存，尚未启用'
              )
            }
          >
            保存为新版本
          </Button>
        </div>
      )}
      {policies.data?.items.map((p) => (
        <article key={p.id} className='flex flex-wrap items-center gap-3 rounded border p-3'>
          <strong>
            {p.name} v{p.version}
          </strong>
          {p.active && <Badge value='当前启用' />}
          <Button
            variant='outline'
            size='sm'
            onClick={() => {
              setName(p.name);
              setDraft(JSON.stringify(p.config, null, 2));
            }}
          >
            载入编辑器
          </Button>
          <Button
            size='sm'
            variant='outline'
            disabled={busy || !selected.length || selected.length > 100}
            onClick={() =>
              void act(async () => {
                setTest(await request(`/policies/${p.id}/test`, 'POST', { product_ids: selected }));
              })
            }
          >
            试运行已选商品
          </Button>
          {session.role === 'owner' && !p.active && (
            <Button
              size='sm'
              disabled={busy}
              onClick={() =>
                void act(async () => {
                  const job = await request<JobCreated>(`/policies/${p.id}/activate`, 'POST');
                  watch(job.job_id);
                }, '已启用，工作区全量初筛已排队')
              }
            >
              启用并全量重筛
            </Button>
          )}
        </article>
      ))}
      {test !== null && <DataBlock title='规则试运行结果（未应用）' value={test} />}
      <Button
        variant='outline'
        disabled={busy}
        onClick={() =>
          void act(async () => {
            const job = await request<JobCreated>(
              '/screen-runs',
              'POST',
              selected.length ? { product_ids: selected } : {}
            );
            watch(job.job_id);
          }, '重新初筛已排队')
        }
      >
        {selected.length ? '重新初筛所选商品' : '重新初筛全部商品'}
      </Button>
    </div>
  );
}
