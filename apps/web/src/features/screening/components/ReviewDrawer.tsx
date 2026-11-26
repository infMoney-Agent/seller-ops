'use client';
import { useState } from 'react';
import Image from 'next/image';
import { Icons } from '@/components/icons';
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle
} from '@/components/ui/sheet';
import { useScreeningQuery } from '../queries';
import { request } from '../service';
import { childSales } from '../sales';
import type { Product } from '../types';
import AsinTools from './AsinTools';
import BilingualProductTitle from './BilingualProductTitle';
import FamilyPanel from './FamilyPanel';
import MonthlyHistory from './MonthlyHistory';
import ProductParameters from './ProductParameters';
import { QuickReasonChips, ReviewDecisionCards, type DecisionStatus } from './QuickReasonChips';
import { Badge, Button, DataBlock, Field, Input, formatMoney, type Action } from './shared';

export default function ReviewDrawer({
  open,
  product,
  act,
  busy,
  onClose,
  onCalculate,
  onChooseFamily
}: {
  open: boolean;
  product: Product | null;
  act: Action;
  busy: boolean;
  onClose: () => void;
  onCalculate: (product: Product) => void;
  onChooseFamily: (ids: string[]) => void;
}) {
  return (
    <Sheet
      open={open && Boolean(product)}
      onOpenChange={(nextOpen) => {
        if (!nextOpen && !busy) onClose();
      }}
    >
      <SheetContent
        side='right'
        className='flex w-full flex-col gap-0 overscroll-contain p-0 sm:max-w-lg'
      >
        <SheetHeader className='border-b px-5 py-4 pr-12'>
          <SheetTitle className='flex items-center gap-2 text-base font-semibold'>
            <span>人工复核</span>
            {product && (
              <span className='text-muted-foreground font-mono text-sm font-normal'>
                · {product.asin}
              </span>
            )}
          </SheetTitle>
          <SheetDescription className='text-xs'>
            按 Esc 或点击遮罩关闭 · 保存时自动校验乐观锁版本防止并发覆盖
          </SheetDescription>
        </SheetHeader>

        {product && open && (
          <ReviewDrawerLoader
            product={product}
            act={act}
            busy={busy}
            onClose={onClose}
            onCalculate={onCalculate}
            onChooseFamily={onChooseFamily}
          />
        )}
      </SheetContent>
    </Sheet>
  );
}

function ReviewDrawerLoader({
  product,
  act,
  busy,
  onClose,
  onCalculate,
  onChooseFamily
}: {
  product: Product;
  act: Action;
  busy: boolean;
  onClose: () => void;
  onCalculate: (product: Product) => void;
  onChooseFamily: (ids: string[]) => void;
}) {
  const detail = useScreeningQuery<Product>(`/products/` + product.id);
  if (detail.isError) {
    return (
      <div className='p-5'>
        <p role='alert' className='text-destructive text-sm'>
          {detail.error.message}
        </p>
      </div>
    );
  }
  const resolved = detail.data || product;
  return (
    <ReviewDrawerBody
      key={`${resolved.id}:${resolved.data_revision}:${resolved.review_revision}`}
      product={resolved}
      loadingDetail={detail.isLoading}
      act={act}
      busy={busy}
      onClose={onClose}
      onCalculate={onCalculate}
      onChooseFamily={onChooseFamily}
    />
  );
}

function ReviewDrawerBody({
  product,
  loadingDetail,
  act,
  busy,
  onClose,
  onCalculate,
  onChooseFamily
}: {
  product: Product;
  loadingDetail: boolean;
  act: Action;
  busy: boolean;
  onClose: () => void;
  onCalculate: (product: Product) => void;
  onChooseFamily: (ids: string[]) => void;
}) {
  const [status, setStatus] = useState<DecisionStatus>(
    product.review_status === 'pending' ? 'kept' : product.review_status
  );
  const [reason, setReason] = useState(product.review_reason || '');
  const [note, setNote] = useState(product.review_note || '');
  const [manualCategory, setManualCategory] = useState(product.manual_category || '');
  const [auditOpen, setAuditOpen] = useState(false);

  const history = useScreeningQuery<unknown>(auditOpen ? `/products/${product.id}/history` : null);
  const observations = useScreeningQuery<unknown>(
    auditOpen ? `/products/${product.id}/observations` : null
  );

  async function submitReview(goToProfit: boolean) {
    await act(async () => {
      await request(`/products/${product.id}/review`, 'PATCH', {
        expected_review_revision: product.review_revision,
        expected_data_revision: product.data_revision,
        status,
        reason,
        note
      });
      onClose();
      if (goToProfit) {
        onCalculate({
          ...product,
          review_status: status,
          review_reason: reason,
          review_note: note,
          review_revision: product.review_revision + 1
        });
      }
    }, '人工结论已保存');
  }

  return (
    <>
      <div className='screening-scroll flex-1 space-y-5 overflow-y-auto overscroll-contain p-5 text-xs'>
        {/* 目标商品摘要卡片 */}
        <section
          aria-label='复核目标商品摘要'
          className='bg-muted/40 flex items-start gap-3 rounded-xl border p-3.5'
        >
          {product.image_url ? (
            <Image
              unoptimized
              width={52}
              height={52}
              src={product.image_url}
              alt=''
              className='size-13 shrink-0 rounded-lg border bg-white object-contain'
              referrerPolicy='no-referrer'
            />
          ) : (
            <div
              aria-hidden='true'
              className='bg-muted text-muted-foreground flex size-13 shrink-0 items-center justify-center rounded-lg border'
            >
              <Icons.product className='size-5 opacity-60' />
            </div>
          )}
          <div className='min-w-0 flex-1 space-y-1.5'>
            <div className='flex flex-wrap items-center gap-1.5'>
              <span className='font-mono text-xs font-semibold'>{product.asin}</span>
              <AsinTools asin={product.asin} />
              {product.parent_asin && (
                <span className='text-muted-foreground font-mono text-[11px]'>
                  父体 {product.parent_asin}
                </span>
              )}
              <Badge value={product.decision} />
              {product.recommendation && <Badge value={product.recommendation} />}
            </div>
            <BilingualProductTitle product={product} compact />
            <div className='text-muted-foreground flex flex-wrap items-center gap-x-3 gap-y-0.5 font-mono text-[11px] tabular-nums'>
              <span>价格 ${formatMoney(product.price_usd)}</span>
              <span>子体销量 {childSales(product)}</span>
              {loadingDetail && <span>同步最新详情…</span>}
            </div>
            {product.reasons && product.reasons.length > 0 && (
              <p className='text-muted-foreground leading-relaxed'>
                机筛依据：{product.reasons.join('；')}
              </p>
            )}
          </div>
        </section>

        {/* 三态卡片式结论选择 */}
        <div className='space-y-2'>
          <span className='text-muted-foreground block text-xs font-medium'>人工结论</span>
          <ReviewDecisionCards value={status} disabled={busy} onChange={setStatus} />
        </div>

        {/* 快捷原因标签 + 原因输入 */}
        <div className='space-y-2'>
          <div className='flex items-center justify-between'>
            <span className='text-muted-foreground text-xs font-medium'>常用原因快捷填入</span>
          </div>
          <QuickReasonChips
            currentReason={reason}
            disabled={busy}
            onPick={(pickedReason, suggestedStatus) => {
              setReason(pickedReason);
              setStatus(suggestedStatus);
            }}
          />
          <Field label={status === 'kept' ? '复核原因' : '复核原因（建议填写淘汰或待确认依据）'}>
            <Input
              value={reason}
              disabled={busy}
              placeholder='填写或点击上方快捷标签填入复核原因…'
              onChange={(e) => setReason(e.target.value)}
            />
          </Field>
        </div>

        {/* 补充备注 */}
        <Field label='补充备注（选填）'>
          <textarea
            rows={3}
            aria-label='补充备注'
            value={note}
            disabled={busy}
            placeholder='记录 1688 货源询价、模具差异或专利排查备注…'
            onChange={(e) => setNote(e.target.value)}
            className='border-input bg-background placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-ring/50 w-full rounded-lg border px-3 py-2 text-xs outline-none focus-visible:ring-[3px]'
          />
        </Field>

        {/* 流转保护说明 */}
        <div className='rounded-xl border border-amber-500/30 bg-amber-500/[0.08] p-3 leading-relaxed'>
          <span className='font-semibold text-amber-700 dark:text-amber-300'>流转保护：</span>
          保存人工结论后，重新执行机器初筛不会覆盖人工判断；选择「保留」可直接点击下方「保存并测算」建立利润方案。
        </div>

        {/* 折叠进阶区：人工分类、商品参数维护、同父体与经营趋势、审计记录 */}
        <details className='rounded-xl border p-3'>
          <summary className='cursor-pointer text-xs font-semibold select-none'>
            人工分类与参数维护
          </summary>
          <div className='mt-3 space-y-3 border-t pt-3'>
            <div className='flex flex-wrap items-end gap-2'>
              <div className='min-w-48 flex-1'>
                <Field label='人工分类'>
                  <Input
                    value={manualCategory}
                    disabled={busy}
                    onChange={(e) => setManualCategory(e.target.value)}
                  />
                </Field>
              </div>
              <Button
                size='sm'
                variant='outline'
                disabled={busy || !manualCategory.trim()}
                onClick={() =>
                  void act(async () => {
                    await request(`/products/${product.id}/category`, 'PATCH', {
                      expected_data_revision: product.data_revision,
                      expected_review_revision: product.review_revision,
                      category: manualCategory
                    });
                    onClose();
                  }, '人工分类已保存')
                }
              >
                保存人工分类
              </Button>
            </div>
            <ProductParameters product={product} act={act} busy={busy} close={onClose} />
          </div>
        </details>

        <details className='rounded-xl border p-3'>
          <summary className='cursor-pointer text-xs font-semibold select-none'>
            同父体变体与分期经营历史
          </summary>
          <div className='mt-3 space-y-3 border-t pt-3'>
            <FamilyPanel
              product={product}
              choose={(ids) => {
                onClose();
                onChooseFamily(ids);
              }}
            />
            <MonthlyHistory
              product={product}
              onCalculate={(p) => {
                onClose();
                onCalculate(p);
              }}
            />
          </div>
        </details>

        <details
          className='rounded-xl border p-3'
          onToggle={(e) => setAuditOpen((e.currentTarget as HTMLDetailsElement).open)}
        >
          <summary className='cursor-pointer text-xs font-semibold select-none'>
            规格原文与审计历史
          </summary>
          <div className='mt-3 space-y-2 border-t pt-3'>
            <DataBlock title='商品规格' value={product.specs} />
            <DataBlock title='商品完整资料（主表）' value={product} />
            <DataBlock title='操作历史' value={history.data} />
            <DataBlock title='来源历史' value={observations.data} />
          </div>
        </details>
      </div>

      <SheetFooter className='bg-card flex-row items-center justify-between gap-2 border-t px-5 py-3.5'>
        <Button type='button' variant='outline' size='sm' disabled={busy} onClick={onClose}>
          取消
        </Button>
        <div className='flex items-center gap-2'>
          <Button
            type='button'
            variant='outline'
            size='sm'
            disabled={busy}
            onClick={() => void submitReview(true)}
          >
            保存并测算
          </Button>
          <Button type='button' size='sm' disabled={busy} onClick={() => void submitReview(false)}>
            {busy ? '保存中…' : '保存复核结论'}
          </Button>
        </div>
      </SheetFooter>
    </>
  );
}
