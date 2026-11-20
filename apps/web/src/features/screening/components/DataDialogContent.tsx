'use client';
import type { ReactNode } from 'react';
import { DialogClose, DialogContent, DialogDescription, DialogTitle } from '@/components/ui/dialog';
import { cn } from '@/lib/utils';
import { Button } from './shared';

export default function DataDialogContent({
  title,
  description,
  wide = false,
  children
}: {
  title: string;
  description: string;
  wide?: boolean;
  children: ReactNode;
}) {
  return (
    <DialogContent
      showCloseButton={false}
      className={cn(
        'flex h-[min(80dvh,48rem)] w-[calc(100%-2rem)] max-w-[calc(100%-2rem)] flex-col gap-0 p-0',
        wide ? 'sm:max-w-4xl' : 'sm:max-w-2xl'
      )}
    >
      <header className='flex shrink-0 items-start justify-between gap-4 border-b px-5 py-4'>
        <div className='min-w-0 space-y-1'>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription className='line-clamp-2 break-words'>{description}</DialogDescription>
        </div>
        <DialogClose
          render={<Button size='sm' variant='ghost' className='shrink-0' />}
          aria-label={'关闭' + title}
        >
          关闭
        </DialogClose>
      </header>
      <div
        data-data-dialog-body
        className='min-h-0 flex-1 overflow-auto overscroll-contain p-5 [scrollbar-gutter:stable]'
      >
        {children}
      </div>
    </DialogContent>
  );
}
