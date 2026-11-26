'use client';
import { useEffect, useRef, useState } from 'react';
import { Dialog } from '@/components/ui/dialog';
import DataDialogContent from './DataDialogContent';
import { Button, Field, Input, type Action } from './shared';
import { QuickReasonChips, ReviewDecisionCards } from './QuickReasonChips';
import { request } from '../service';
import type { BatchPreview, BatchResult, Product } from '../types';

export default function BatchReviewDialog({
  open,
  onClose,
  selected,
  act,
  busy,
  onApplied,
  reference,
  excludeFamilies = false
}: {
  open: boolean;
  onClose: () => void;
  selected: string[];
  act: Action;
  busy: boolean;
  onApplied: () => void;
  reference?: Product | null;
  excludeFamilies?: boolean;
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
        description={'已选 ' + selected.length + ' 项；默认仅修改待复核商品，可勾选覆盖已有结论'}
      >
        <BatchReviewForm
          selected={selected}
          reference={reference}
          excludeFamilies={excludeFamilies}
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
  done,
  reference,
  excludeFamilies
}: {
  selected: string[];
  act: Action;
  busy: boolean;
  done: () => void;
  reference?: Product | null;
  excludeFamilies: boolean;
}) {
  const [mode, setMode] = useState(reference ? 'reference' : 'direct');
  const [overwrite, setOverwrite] = useState(false);
  const [status, setStatus] = useState('kept'),
    [reason, setReason] = useState(''),
    [note, setNote] = useState('');
  const [submitting, setSubmitting] = useState(false),
    [saveError, setSaveError] = useState('');
  const submittingRef = useRef(false),
    errorRef = useRef<HTMLParagraphElement>(null);
  const preparedBatch = useRef<BatchPreview | null>(null);
  const disabled = busy || submitting;
  useEffect(() => {
    if (saveError) errorRef.current?.focus();
  }, [saveError]);
  function edited() {
    preparedBatch.current = null;
    setSaveError('');
  }
  return (
    <form
      method='post'
      aria-label='已选商品批量复核'
      aria-busy={submitting}
      className='space-y-3'
      onSubmit={(event) => {
        event.preventDefault();
        if (disabled || submittingRef.current) return;
        submittingRef.current = true;
        setSubmitting(true);
        setSaveError('');
        void act(async () => {
          try {
            // Snapshot/version checks remain internal; saving is one user action.
            let batch = preparedBatch.current;
            if (!batch || Date.parse(batch.expires_at) <= Date.now()) {
              batch = await request<BatchPreview>('/review-batches/preview', 'POST', {
                product_ids: selected,
                scope:
                  excludeFamilies &&
                  (mode === 'reference' ? reference?.review_status : status) === 'excluded'
                    ? 'selected_families'
                    : 'selected',
                mode,
                reference_product_id: mode === 'reference' ? reference?.id : null,
                action: 'review',
                overwrite,
                status,
                reason,
                note
              });
              preparedBatch.current = batch;
            }
            if (!batch.targets.some((target) => target.eligible))
              throw new Error(
                overwrite
                  ? '所选商品均无需修改，请刷新列表后重新选择。'
                  : '所选商品均已有人工结论；如需覆盖请勾选下方选项。'
              );
            const result = await request<BatchResult>(
              '/review-batches/' + batch.id + '/apply',
              'POST'
            );
            if (!result.success) throw new Error('商品数据已变化，本次未保存，请刷新列表后重试。');
            done();
          } catch (error) {
            if (!(error instanceof TypeError)) preparedBatch.current = null;
            setSaveError(error instanceof Error ? error.message : '保存失败，请重试。');
            throw error;
          }
        }, '批量复核已保存').finally(() => {
          submittingRef.current = false;
          setSubmitting(false);
        });
      }}
    >
      <div className='flex items-start gap-2'>
        <input
          id='batch-review-overwrite'
          name='overwrite'
          aria-label='覆盖已有人工结论'
          type='checkbox'
          className='accent-primary mt-1 size-4'
          checked={overwrite}
          disabled={disabled}
          onChange={(event) => {
            setOverwrite(event.target.checked);
            edited();
          }}
        />
        <label htmlFor='batch-review-overwrite' className='text-sm'>
          覆盖已有人工结论
          <span className='text-muted-foreground block text-xs'>
            修改前的结果会保留在操作历史中，可按需撤销；未勾选时仅修改待复核商品。
          </span>
        </label>
      </div>
      {reference && (
        <Field label='复核方式'>
          <select
            name='mode'
            aria-label='批量复核方式'
            className='h-10 rounded border bg-background px-3'
            disabled={disabled}
            value={mode}
            onChange={(e) => {
              setMode(e.target.value);
              edited();
            }}
          >
            <option value='direct'>填写复核结论</option>
            <option value='reference'>采用参考样本 {reference.asin}</option>
          </select>
        </Field>
      )}
      {mode === 'direct' && (
        <ReviewDecisionCards
          value={status}
          disabled={disabled}
          onChange={(next) => {
            setStatus(next);
            edited();
          }}
        />
      )}
      <Field label='结论'>
        <select
          name='status'
          className='h-10 rounded border bg-background px-3'
          value={mode === 'reference' ? reference?.review_status : status}
          disabled={disabled || mode === 'reference'}
          onChange={(e) => {
            setStatus(e.target.value);
            edited();
          }}
        >
          <option value='kept'>保留</option>
          <option value='excluded'>排除</option>
          <option value='unsure'>待确认</option>
        </select>
      </Field>
      {mode === 'direct' && (
        <QuickReasonChips
          currentReason={reason}
          disabled={disabled}
          onPick={(pickedReason, suggestedStatus) => {
            setReason(pickedReason);
            setStatus(suggestedStatus);
            edited();
          }}
        />
      )}
      <Field label={mode === 'reference' || status === 'kept' ? '原因' : '原因（必填）'}>
        <Input
          name='reason'
          required={mode === 'direct' && status !== 'kept'}
          disabled={disabled || mode === 'reference'}
          value={mode === 'reference' ? reference?.review_reason || '' : reason}
          onChange={(e) => {
            setReason(e.target.value);
            edited();
          }}
        />
      </Field>
      <Field label='备注'>
        <Input
          name='note'
          disabled={disabled || mode === 'reference'}
          value={mode === 'reference' ? reference?.review_note || '' : note}
          onChange={(e) => {
            setNote(e.target.value);
            edited();
          }}
        />
      </Field>
      {selected.length > 100 && (
        <p role='alert' className='text-sm text-destructive'>
          每批最多100项，请减少勾选。
        </p>
      )}
      {saveError && (
        <p ref={errorRef} tabIndex={-1} role='alert' className='text-sm text-destructive'>
          {saveError}
        </p>
      )}
      <Button type='submit' disabled={disabled || !selected.length || selected.length > 100}>
        {submitting ? '保存中…' : '保存修改'}
      </Button>
    </form>
  );
}
