'use client';
import Image from 'next/image';
import type { ReactNode } from 'react';
import { cn } from '@/lib/utils';
import type { Product } from '../types';
import { childSales } from '../sales';
import { Badge, formatMoney } from './shared';

function size(p: Product, packaging = false) {
  const dimensions = packaging
    ? [p.package_length_cm, p.package_width_cm, p.package_height_cm]
    : [p.product_length_cm, p.product_width_cm, p.product_height_cm];
  return dimensions.every((v) => v != null)
    ? dimensions
        .map((v) => Number(v).toLocaleString('zh-CN', { maximumFractionDigits: 2 }))
        .join(' × ') + ' cm'
    : (packaging
        ? p.package_dimensions || p.package_dimensions_original
        : p.product_dimensions || p.product_dimensions_original) || '—';
}
export default function ReviewProductCard({
  product: p,
  compact = false,
  checked,
  disabled = false,
  onCheck,
  children
}: {
  product: Product;
  compact?: boolean;
  checked?: boolean;
  disabled?: boolean;
  onCheck?: (checked: boolean) => void;
  children?: ReactNode;
}) {
  return (
    <article
      data-review-product={p.asin}
      className={cn(
        '@container/card min-w-0 rounded-xl border bg-background p-4 transition-colors',
        checked && 'border-primary bg-primary/5 ring-1 ring-primary/20'
      )}
    >
      {onCheck && (
        <div className='mb-3 flex items-center justify-between gap-2'>
          <label className='flex min-h-10 cursor-pointer items-center gap-2 text-sm'>
            <input
              type='checkbox'
              className='size-4 accent-primary'
              checked={Boolean(checked)}
              disabled={disabled}
              onChange={(e) => onCheck(e.target.checked)}
              aria-label={'勾选 ' + p.asin}
            />
            {p.review_status !== 'pending' ? '已有人工结论' : '选择商品'}
          </label>
          <Badge value={p.review_status} />
        </div>
      )}
      <div
        className={cn(
          'grid min-w-0 gap-3',
          compact ? 'grid-cols-[4rem_minmax(0,1fr)]' : '@lg/card:grid-cols-[9rem_minmax(0,1fr)]'
        )}
      >
        {p.image_url?.startsWith('https://') ? (
          <Image
            unoptimized
            src={p.image_url}
            width={compact ? 64 : 160}
            height={compact ? 64 : 160}
            alt={p.title}
            referrerPolicy='no-referrer'
            loading='lazy'
            className={cn(
              'block rounded-lg bg-white object-contain',
              compact ? 'size-16' : 'aspect-square w-36 max-w-full'
            )}
          />
        ) : (
          <div
            className={cn(
              'grid place-items-center rounded-lg bg-muted text-xs text-muted-foreground',
              compact ? 'size-16' : 'size-36'
            )}
            aria-label='暂无商品图片'
          >
            暂无图片
          </div>
        )}
        <div className='min-w-0 space-y-2'>
          <div className='flex flex-wrap items-center gap-2'>
            <h4 className='font-semibold'>
              <a
                href={'https://www.amazon.com/dp/' + p.asin}
                target='_blank'
                rel='noopener noreferrer'
                className='underline-offset-4 hover:underline'
              >
                {p.asin}
              </a>
            </h4>
            {!onCheck && <Badge value={p.review_status} />}
          </div>
          <p
            className={cn(
              'break-words leading-relaxed',
              compact ? 'line-clamp-3 text-xs' : 'text-sm font-medium'
            )}
          >
            {p.title_zh || p.title}
          </p>
          {!compact && p.title_zh && (
            <p className='text-xs leading-relaxed text-muted-foreground'>{p.title}</p>
          )}
          {!compact && (
            <dl className='grid grid-cols-2 gap-x-4 gap-y-2 text-xs @xl/card:grid-cols-3'>
              {(
                [
                  ['价格 USD', formatMoney(p.price_usd)],
                  ['子体销量', childSales(p)],
                  ['材质', p.material || '—'],
                  ['商品尺寸', size(p)],
                  ['包装尺寸', size(p, true)],
                  [
                    '商品重量',
                    p.weight_g != null ? Number(p.weight_g).toLocaleString('zh-CN') + ' g' : '—'
                  ],
                  ['卖家', p.seller_name || p.seller_id || '—'],
                  ['卖家地区', p.seller_region || '—'],
                  ['父 ASIN', p.parent_asin || '—']
                ] as const
              ).map(([label, value]) => (
                <div key={label} className='min-w-0'>
                  <dt className='text-muted-foreground'>{label}</dt>
                  <dd className='mt-0.5 break-words'>{value}</dd>
                </div>
              ))}
            </dl>
          )}
          {p.review_reason && (
            <p className='break-words text-xs leading-relaxed text-muted-foreground'>
              原因：{p.review_reason}
            </p>
          )}
          {p.review_note && !compact && (
            <p className='text-xs text-muted-foreground'>备注：{p.review_note}</p>
          )}
        </div>
      </div>
      {children && (
        <footer className='mt-3 flex flex-wrap items-center gap-2 border-t pt-3'>{children}</footer>
      )}
    </article>
  );
}
