'use client';
import { cn } from '@/lib/utils';
import type { ReviewStatus } from '../types';

export type DecisionStatus = Exclude<ReviewStatus, 'pending'>;

export const DECISION_OPTIONS: {
  value: DecisionStatus;
  label: string;
  hint: string;
  accentClass: string;
  dotClass: string;
}[] = [
  {
    value: 'kept',
    label: '保留',
    hint: '进入选品池与测算',
    accentClass: 'text-emerald-700 dark:text-emerald-300',
    dotClass: 'bg-emerald-500'
  },
  {
    value: 'excluded',
    label: '排除',
    hint: '标记淘汰并记录原因',
    accentClass: 'text-red-700 dark:text-red-300',
    dotClass: 'bg-red-500'
  },
  {
    value: 'unsure',
    label: '待确认',
    hint: '二次核实货源或风险',
    accentClass: 'text-amber-700 dark:text-amber-300',
    dotClass: 'bg-amber-500'
  }
];

export const QUICK_REVIEW_REASONS: {
  label: string;
  suggestedStatus: DecisionStatus;
}[] = [
  { label: '符合选品标准，进入测算', suggestedStatus: 'kept' },
  { label: '未找到供货商', suggestedStatus: 'excluded' },
  { label: '无差异化优势', suggestedStatus: 'excluded' },
  { label: '侵权/外观专利风险', suggestedStatus: 'unsure' },
  { label: '利润空间不足', suggestedStatus: 'excluded' },
  { label: '季节性波动过大', suggestedStatus: 'unsure' }
];

export function ReviewDecisionCards({
  value,
  disabled,
  onChange
}: {
  value: string;
  disabled?: boolean;
  onChange: (next: DecisionStatus) => void;
}) {
  return (
    <div className='grid grid-cols-3 gap-2' role='radiogroup' aria-label='人工复核结论选择'>
      {DECISION_OPTIONS.map((option) => {
        const active = value === option.value;
        return (
          <button
            key={option.value}
            type='button'
            role='radio'
            aria-checked={active}
            disabled={disabled}
            onClick={() => onChange(option.value)}
            className={cn(
              'flex flex-col gap-1 rounded-xl border p-2.5 text-left text-xs transition-all disabled:pointer-events-none disabled:opacity-50',
              active
                ? 'border-primary bg-muted/70 ring-primary/20 ring-2'
                : 'border-border bg-background hover:bg-muted/40'
            )}
          >
            <span className='flex w-full items-center justify-between gap-1'>
              <span className={cn('font-semibold', option.accentClass)}>{option.label}</span>
              <span aria-hidden='true' className={cn('size-2 rounded-full', option.dotClass)} />
            </span>
            <span className='text-muted-foreground leading-snug'>{option.hint}</span>
          </button>
        );
      })}
    </div>
  );
}

export function QuickReasonChips({
  currentReason,
  disabled,
  onPick
}: {
  currentReason: string;
  disabled?: boolean;
  onPick: (reason: string, suggestedStatus: DecisionStatus) => void;
}) {
  return (
    <div className='flex flex-wrap gap-1.5' aria-label='常用复核原因快捷标签'>
      {QUICK_REVIEW_REASONS.map((chip) => {
        const active = currentReason.trim() === chip.label;
        return (
          <button
            key={chip.label}
            type='button'
            disabled={disabled}
            onClick={() => onPick(chip.label, chip.suggestedStatus)}
            className={cn(
              'rounded-full border px-2.5 py-1 text-xs transition-colors disabled:pointer-events-none disabled:opacity-50',
              active
                ? 'border-primary bg-primary/10 text-foreground font-medium'
                : 'border-border bg-background text-muted-foreground hover:bg-muted hover:text-foreground'
            )}
          >
            {chip.label}
          </button>
        );
      })}
    </div>
  );
}
