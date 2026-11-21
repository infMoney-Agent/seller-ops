'use client';
import { useState } from 'react';
import { Dialog } from '@/components/ui/dialog';
import DataDialogContent from './DataDialogContent';
import { Badge, Button, Field, Input, type Action } from './shared';
import { request } from '../service';
import type { BatchPreview, BatchResult } from '../types';
export default function BatchReviewDialog({
  open,
  onClose,
  selected,
  act,
  busy,
  onApplied
}: {
  open: boolean;
  onClose: () => void;
  selected: string[];
  act: Action;
  busy: boolean;
  onApplied: () => void;
}) {
  if (!open) return null;
  return (
    <Dialog
      open
      onOpenChange={(value) => {
        if (!value && !busy) onClose();
      }}
    >
      <DataDialogContent
        closeDisabled={busy}
        title='批量复核已选商品'
        description={'已选 ' + selected.length + ' 项；仅修改待复核商品'}
      >
        <BatchReviewForm
          selected={selected}
          act={act}
          busy={busy}
          done={() => {
            onApplied();
            onClose();
          }}
        />
      </DataDialogContent>
    </Dialog>
  );
}
function BatchReviewForm({
  selected,
  act,
  busy,
  done
}: {
  selected: string[];
  act: Action;
  busy: boolean;
  done: () => void;
}) {
  const [status, setStatus] = useState('kept'),
    [reason, setReason] = useState(''),
    [note, setNote] = useState('');
  const [preview, setPreview] = useState<BatchPreview | null>(null);
  return (
    <div className='space-y-4'>
      <form
        aria-label='已选商品批量复核'
        className='space-y-3'
        onSubmit={(e) => {
          e.preventDefault();
          void act(async () => {
            setPreview(
              await request<BatchPreview>('/review-batches/preview', 'POST', {
                product_ids: selected,
                scope: 'selected',
                mode: 'direct',
                action: 'review',
                status,
                reason,
                note
              })
            );
          }, '请核对批量预览');
        }}
      >
        <Field label='结论'>
          <select
            className='h-10 rounded border bg-background px-3'
            value={status}
            disabled={busy}
            onChange={(e) => {
              setStatus(e.target.value);
              setPreview(null);
            }}
          >
            <option value='kept'>保留</option>
            <option value='excluded'>排除</option>
            <option value='unsure'>待确认</option>
          </select>
        </Field>
        <Field label={status === 'kept' ? '原因' : '原因（必填）'}>
          <Input
            required={status !== 'kept'}
            disabled={busy}
            value={reason}
            onChange={(e) => {
              setReason(e.target.value);
              setPreview(null);
            }}
          />
        </Field>
        <Field label='备注'>
          <Input
            disabled={busy}
            value={note}
            onChange={(e) => {
              setNote(e.target.value);
              setPreview(null);
            }}
          />
        </Field>
        {selected.length > 100 && (
          <p role='alert' className='text-sm text-destructive'>
            每批最多100项，请减少勾选。
          </p>
        )}
        <Button type='submit' disabled={busy || !selected.length || selected.length > 100}>
          预览批量复核
        </Button>
      </form>
      {preview && (
        <section className='space-y-3 rounded border p-3' aria-label='已选商品批量预览'>
          <div className='flex items-center gap-2'>
            <Badge value={preview.status} />
            <span className='text-sm'>
              将保存 {preview.targets.filter((p) => p.eligible).length} 项
            </span>
          </div>
          <p className='text-sm'>{preview.reason}</p>
          <ul className='space-y-2 text-xs'>
            {preview.targets.map((p) => (
              <li key={p.id}>
                {p.asin} · {p.title} · {p.eligible ? '将保存' : p.skip_reason}
              </li>
            ))}
          </ul>
          <Button
            disabled={busy || !preview.targets.some((p) => p.eligible)}
            onClick={() =>
              void act(async () => {
                await request<BatchResult>('/review-batches/' + preview.id + '/apply', 'POST');
                done();
              }, '批量复核已保存')
            }
          >
            确认保存本批修改
          </Button>
        </section>
      )}
    </div>
  );
}
