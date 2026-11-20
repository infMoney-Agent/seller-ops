'use client';
import { useState } from 'react';
import {
  Popover,
  PopoverTrigger,
  PopoverContent,
  PopoverTitle,
  PopoverDescription
} from '@/components/ui/popover';
import { useScreeningQuery } from '../queries';
import type { Json } from '../types';
import { Button, TableScroll } from './shared';
type Source = {
  id: string;
  import_id: string;
  file_name: string;
  member_path: string;
  sheet_name: string;
  row_no: number;
  raw_data: Record<string, Json>;
};
export default function SourceRowCard({ productID, rowID }: { productID: string; rowID: string }) {
  const [open, setOpen] = useState(false);
  const source = useScreeningQuery<Source>(
    open ? '/products/' + productID + '/observations/' + rowID : null
  );
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger className='text-primary mt-1 text-xs underline underline-offset-2'>
        查看来源文件
      </PopoverTrigger>
      <PopoverContent
        align='end'
        className='max-h-[65vh] w-[min(36rem,calc(100vw-2rem))] overflow-y-auto p-4'
      >
        <div className='flex items-start justify-between gap-3'>
          <div className='min-w-0'>
            <PopoverTitle>来源原始数据</PopoverTitle>
            <PopoverDescription>对应当前经营记录的原始表格行</PopoverDescription>
          </div>
          <Button size='sm' variant='ghost' onClick={() => setOpen(false)}>
            关闭
          </Button>
        </div>
        {source.isLoading && (
          <p role='status' className='mt-3 text-sm'>
            正在加载原始数据…
          </p>
        )}
        {source.isError && (
          <p role='alert' className='text-destructive mt-3 text-sm'>
            {source.error.message}
          </p>
        )}
        {source.data && (
          <div className='mt-3 space-y-3'>
            <div className='space-y-1 rounded-lg bg-muted/50 p-3 text-xs'>
              <a
                className='text-primary break-words underline underline-offset-2'
                href={'/api/ecom/screening/imports/' + source.data.import_id + '/source'}
                target='_blank'
                rel='noopener noreferrer'
              >
                {source.data.file_name} · 打开 / 下载原文件
              </a>
              <p>
                {source.data.sheet_name || 'CSV'} · 第 {source.data.row_no} 行
              </p>
              {source.data.member_path && source.data.member_path !== source.data.file_name && (
                <p className='break-words'>{source.data.member_path}</p>
              )}
            </div>
            <TableScroll label='来源原始行数据，可滚动'>
              <table className='w-full text-left text-xs'>
                <caption className='sr-only'>原始文件中的字段和值</caption>
                <thead className='bg-muted'>
                  <tr>
                    <th scope='col' className='p-2'>
                      原始列名
                    </th>
                    <th scope='col' className='p-2'>
                      原始值
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {Object.entries(source.data.raw_data).map(([key, value]) => (
                    <tr key={key} className='border-t align-top'>
                      <th scope='row' className='w-36 p-2 font-medium'>
                        {key}
                      </th>
                      <td className='max-w-80 whitespace-pre-wrap break-words p-2'>
                        {value == null || value === ''
                          ? '—'
                          : typeof value === 'object'
                            ? JSON.stringify(value)
                            : String(value)}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </TableScroll>
          </div>
        )}
      </PopoverContent>
    </Popover>
  );
}
