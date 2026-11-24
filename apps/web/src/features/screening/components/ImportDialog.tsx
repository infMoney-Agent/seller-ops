'use client';
import { useEffect, useId, useRef, useState } from 'react';
import ImportPanel from './ImportPanel';
import { Button, type Action } from './shared';
import type { Job } from '../types';
export default function ImportDialog({
  open,
  onClose,
  act,
  watch,
  busy,
  error,
  message,
  job,
  onRemoved
}: {
  open: boolean;
  onClose: () => void;
  act: Action;
  watch: (id: string) => void;
  busy: boolean;
  error: string;
  message: string;
  job: Job | undefined;
  onRemoved?: (id: string) => void;
}) {
  const dialog = useRef<HTMLDialogElement>(null);
  const title = useId();
  const description = useId();
  const [started, setStarted] = useState(open);
  if (open && !started) setStarted(true);
  useEffect(() => {
    if (open) {
      if (!dialog.current?.open) dialog.current?.showModal();
    } else if (dialog.current?.open) {
      dialog.current.close();
    }
  }, [open]);
  return (
    <dialog
      ref={dialog}
      aria-labelledby={title}
      aria-describedby={description}
      onClose={() => {
        if (!dialog.current?.open) onClose();
      }}
      className='m-auto max-h-[85dvh] w-[calc(100%-2rem)] max-w-3xl overflow-y-auto overscroll-contain rounded-xl border bg-background p-0 text-foreground shadow-xl backdrop:bg-black/30 backdrop:overflow-hidden backdrop:overscroll-contain'
    >
      <div className='sticky top-0 z-10 flex items-start justify-between gap-4 border-b bg-background p-4'>
        <div>
          <h2 id={title} className='text-lg font-semibold'>
            导入商品
          </h2>
          <p id={description} className='text-muted-foreground mt-1 text-xs'>
            上传卖家精灵文件，检查后确认导入。关闭卡片后仍会继续处理，可在历史导入记录查看结果。
          </p>
        </div>
        <Button variant='ghost' size='sm' onClick={onClose}>
          关闭
        </Button>
      </div>
      <div className='space-y-3 p-4'>
        {error && (
          <p role='alert' className='text-destructive text-sm'>
            {error}
          </p>
        )}
        {message && (
          <p role='status' className='text-muted-foreground text-sm'>
            {message}
          </p>
        )}
        {job?.error && (
          <p role='alert' className='text-destructive text-sm'>
            {job.error.message}
          </p>
        )}
        {started && <ImportPanel act={act} watch={watch} busy={busy} onRemoved={onRemoved} />}
      </div>
    </dialog>
  );
}
