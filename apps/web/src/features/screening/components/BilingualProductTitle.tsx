import type { Product } from '../types';
import { cn } from '@/lib/utils';

export default function BilingualProductTitle({
  product,
  compact = false,
  className
}: {
  product: Pick<Product, 'title' | 'title_zh'>;
  compact?: boolean;
  className?: string;
}) {
  const original = product.title.trim();
  const translated = product.title_zh?.trim() || '';
  const chinese = /[\u3400-\u9fff]/u.test(translated) ? translated : '';
  const separateOriginal = original && original !== chinese;
  return (
    <div
      className={cn('min-w-0 space-y-1 text-left', className)}
      data-product-title
      data-bilingual={Boolean(chinese && separateOriginal)}
    >
      {chinese && (
        <p
          lang='zh-CN'
          className={cn(
            'line-clamp-2 break-words leading-5',
            compact ? 'text-xs font-medium' : 'text-sm font-medium'
          )}
          title={chinese}
        >
          {chinese}
        </p>
      )}
      {separateOriginal && (
        <p
          lang={/[\u3400-\u9fff]/u.test(original) ? undefined : 'en'}
          className={cn(
            'line-clamp-2 break-words leading-5',
            chinese ? 'text-xs text-muted-foreground' : compact ? 'text-xs' : 'text-sm'
          )}
          title={original}
        >
          {original}
        </p>
      )}
    </div>
  );
}
