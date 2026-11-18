'use client';
import { useState } from 'react';
import Image from 'next/image';
import { useScreeningQuery } from '../queries';
import { request } from '../service';
import type { BatchPreview, BatchRecord, BatchResult, PageResult, Product } from '../types';
import { Badge, Button, DataBlock, Field, Input, type Action } from './shared';

export function ProductPicture({ product }: { product: Product }) {
  return (
    <div className='flex items-start gap-3'>
      {product.image_url?.startsWith('https://') && (
        <Image
          unoptimized
          width={96}
          height={96}
          src={product.image_url}
          alt={product.title}
          className='h-24 w-24 shrink-0 rounded bg-white object-contain'
          loading='lazy'
          referrerPolicy='no-referrer'
        />
      )}
      <div>
        <strong>{product.asin}</strong>
        <p className='text-sm'>{product.title_zh || product.title}</p>
        <p className='text-muted-foreground text-xs'>{product.direction}</p>
        <Badge value={product.review_status} />
        <p className='text-sm'>{product.review_reason}</p>
      </div>
    </div>
  );
}

export default function ReviewPanel({
  selected,
  act,
  busy
}: {
  selected: string[];
  act: Action;
  busy: boolean;
}) {
  const [mode, setMode] = useState('direct');
  const [status, setStatus] = useState('kept');
  const [reason, setReason] = useState('');
  const [note, setNote] = useState('');
  const [reference, setReference] = useState<string | null>(null);
  const [compare, setCompare] = useState<string[]>([]);
  const [preview, setPreview] = useState<BatchPreview | null>(null);
  const [result, setResult] = useState<BatchResult | null>(null);
  const references = useScreeningQuery<{ items: Product[] }>(
    selected[0] ? `/products/${selected[0]}/references` : null
  );
  const history = useScreeningQuery<PageResult<BatchRecord>>('/review-batches');
  const [imagePage, setImagePage] = useState(1);
  const [imagesOpen, setImagesOpen] = useState(false);
  const ids = selected.slice((imagePage - 1) * 50, imagePage * 50);
  return (
    <div className='space-y-4'>
      <p className='text-sm'>
        已跨页选择 {selected.length} 个商品。每批最多 100 个，仅处理待复核商品。先预览，再确认。
      </p>
      <div className='grid gap-3 md:grid-cols-3'>
        <Field label='复核方式'>
          <select
            className='rounded border bg-transparent p-2'
            value={mode}
            onChange={(e) => {
              setMode(e.target.value);
              setPreview(null);
            }}
          >
            <option value='direct'>直接填写结论</option>
            <option value='reference'>采用人工样本</option>
          </select>
        </Field>
        <Field label='结论'>
          <select
            className='rounded border bg-transparent p-2'
            value={status}
            onChange={(e) => {
              setStatus(e.target.value);
              setPreview(null);
            }}
            disabled={mode === 'reference'}
          >
            <option value='kept'>保留</option>
            <option value='excluded'>排除</option>
            <option value='unsure'>待确认</option>
          </select>
        </Field>
        <Field label='原因'>
          <Input
            value={reason}
            disabled={mode === 'reference'}
            onChange={(e) => {
              setReason(e.target.value);
              setPreview(null);
            }}
          />
        </Field>
        <Field label='备注'>
          <Input
            value={note}
            disabled={mode === 'reference'}
            onChange={(e) => {
              setNote(e.target.value);
              setPreview(null);
            }}
          />
        </Field>
      </div>
      <details className='rounded border p-3' open={mode === 'reference'}>
        <summary>人工参考样本（可折叠；勾选两个样本比较）</summary>
        {!references.data?.items.length && (
          <p className='text-muted-foreground py-3 text-sm'>
            所选首个商品暂无同方向人工样本，可直接填写复核结论。
          </p>
        )}
        <div className='grid gap-3 pt-3 md:grid-cols-2'>
          {references.data?.items.map((p) => (
            <article key={p.id} className='space-y-2 rounded border p-3'>
              <ProductPicture product={p} />
              <div className='flex items-center gap-3'>
                <Button
                  size='sm'
                  variant={reference === p.id ? 'default' : 'outline'}
                  onClick={() => {
                    setReference(p.id);
                    setMode('reference');
                    setPreview(null);
                  }}
                >
                  采用此样本
                </Button>
                <label className='text-sm'>
                  <input
                    type='checkbox'
                    checked={compare.includes(p.id)}
                    onChange={(e) =>
                      setCompare((old) =>
                        e.target.checked
                          ? [...old.filter((id) => id !== p.id).slice(-1), p.id]
                          : old.filter((id) => id !== p.id)
                      )
                    }
                  />{' '}
                  比较
                </label>
              </div>
            </article>
          ))}
        </div>
      </details>
      {compare.length === 2 && (
        <div className='grid gap-4 md:grid-cols-2'>
          {compare.map((id) => (
            <LazyProduct key={id} id={id} />
          ))}
        </div>
      )}
      <div className='flex gap-2'>
        <Button
          disabled={
            busy ||
            !selected.length ||
            selected.length > 100 ||
            (mode === 'reference' && !reference)
          }
          onClick={() =>
            void act(async () => {
              const p = await request<BatchPreview>('/review-batches/preview', 'POST', {
                product_ids: selected,
                mode,
                reference_product_id: reference,
                status,
                reason,
                note
              });
              setPreview(p);
              setResult(null);
            }, '预览已生成，请核对后确认')
          }
        >
          生成批量预览
        </Button>
      </div>
      {preview && (
        <section className='space-y-2 rounded border p-4'>
          <h3 className='font-semibold'>
            即将采用：
            <Badge value={preview.status} />
          </h3>
          <p>{preview.reason}</p>
          <p className='text-muted-foreground text-sm'>
            有效至 {new Date(preview.expires_at).toLocaleString()}
          </p>
          {preview.targets.map((p) => (
            <p key={p.id} className='text-sm'>
              {p.asin} · {p.title} · {p.eligible ? '将保存' : p.skip_reason}
            </p>
          ))}
          <Button
            disabled={busy}
            onClick={() =>
              void act(async () => {
                setResult(
                  await request<BatchResult>(`/review-batches/${preview.id}/apply`, 'POST')
                );
                setPreview(null);
              }, '批量复核已执行')
            }
          >
            确认保存人工结论
          </Button>
        </section>
      )}
      {result && (
        <DataBlock
          title={`处理结果：成功 ${result.success}，跳过 ${result.skipped}`}
          value={result}
        />
      )}
      <details className='rounded border p-3' onToggle={(e) => setImagesOpen(e.currentTarget.open)}>
        <summary>已选商品图片比较（每页50个，展开后加载规格）</summary>
        {imagesOpen && (
          <div className='grid gap-3 py-3 md:grid-cols-2'>
            {ids.map((id) => (
              <LazyProduct key={id} id={id} />
            ))}
          </div>
        )}
        <Button
          variant='outline'
          disabled={imagePage <= 1}
          onClick={() => setImagePage((v) => v - 1)}
        >
          上一页
        </Button>
        <Button
          variant='outline'
          disabled={imagePage * 50 >= selected.length}
          onClick={() => setImagePage((v) => v + 1)}
        >
          下一页
        </Button>
      </details>
      <h3 className='font-semibold'>批量操作历史</h3>
      {history.data?.items.map((b) => (
        <div key={b.id} className='flex flex-wrap items-center gap-3 rounded border p-3'>
          <span>{new Date(b.created_at).toLocaleString()}</span>
          <Badge value={b.state} />
          <span className='text-sm'>
            {b.result ? `成功 ${b.result.success}，跳过 ${b.result.skipped}` : '尚未执行'}
          </span>
          {b.state === 'applied' && (
            <Button
              size='sm'
              variant='outline'
              disabled={busy}
              onClick={() =>
                void act(async () => {
                  setResult(await request<BatchResult>(`/review-batches/${b.id}/undo`, 'POST'));
                }, '撤销已完成，后续修改不会覆盖')
              }
            >
              撤销此批次
            </Button>
          )}
        </div>
      ))}
    </div>
  );
}

export function LazyProduct({ id }: { id: string }) {
  const product = useScreeningQuery<Product>('/products/' + id);
  const [expand, setExpand] = useState(false);
  return (
    <article className='space-y-2 rounded border p-3'>
      {product.isError ? (
        <p role='alert'>{product.error.message}</p>
      ) : product.data ? (
        <>
          <ProductPicture product={product.data} />
          <Button variant='ghost' size='sm' onClick={() => setExpand((v) => !v)}>
            {expand ? '收起规格' : '展开规格'}
          </Button>
          {expand && <DataBlock title='详细规格' value={product.data.specs} />}
        </>
      ) : (
        '加载中…'
      )}
    </article>
  );
}
