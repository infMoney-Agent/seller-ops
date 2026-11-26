'use client';
import { useEffect, useRef, useState } from 'react';
import { Icons } from '@/components/icons';
import { Button } from './shared';

let lastCopyFeedback = '';

export default function AsinTools({ asin }: { asin: string }) {
  const [status, setStatus] = useState<'idle' | 'copied' | 'error'>('idle');
  const [announcement, setAnnouncement] = useState('');
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current);
    };
  }, []);

  function report(ok: boolean) {
    if (timerRef.current) clearTimeout(timerRef.current);
    let text = ok ? 'ASIN 已复制' : '复制失败，请手动选择 ASIN';
    if (text === lastCopyFeedback) text += '\u00A0';
    lastCopyFeedback = text;
    setStatus(ok ? 'copied' : 'error');
    setAnnouncement('');
    requestAnimationFrame(() => setAnnouncement(text));
    timerRef.current = setTimeout(() => {
      setStatus('idle');
      setAnnouncement('');
    }, 1800);
  }

  return (
    <span className='relative inline-flex items-center gap-0.5'>
      <Button
        type='button'
        size='sm'
        variant='ghost'
        data-copied={status === 'copied' ? '' : undefined}
        data-failed={status === 'error' ? '' : undefined}
        className='min-h-6 min-w-6 h-6 w-6 p-0'
        aria-label={`复制 ASIN ${asin}`}
        title={status === 'copied' ? '已复制 ASIN' : '复制 ASIN'}
        onClick={async () => {
          try {
            await navigator.clipboard.writeText(asin);
            report(true);
          } catch {
            report(false);
          }
        }}
      >
        {status === 'copied' ? (
          <Icons.check aria-hidden='true' className='size-3.5 text-emerald-600' />
        ) : (
          <Icons.ecomCopy aria-hidden='true' className='text-muted-foreground size-3.5' />
        )}
      </Button>
      <span
        role='status'
        className={
          status === 'error'
            ? 'text-destructive text-xs opacity-100 transition-opacity'
            : 'sr-only opacity-100'
        }
      >
        {announcement}
      </span>
      <a
        href={`https://www.amazon.com/dp/${asin}`}
        target='_blank'
        rel='noopener noreferrer'
        aria-label={`在 Amazon 查看 ${asin}`}
        title='在 Amazon 查看'
        className='text-muted-foreground hover:bg-muted hover:text-foreground inline-flex min-h-6 min-w-6 size-6 items-center justify-center rounded-md transition-colors'
      >
        <Icons.externalLink aria-hidden='true' className='size-3.5' />
      </a>
    </span>
  );
}
