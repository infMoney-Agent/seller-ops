'use client';
import { useEffect } from 'react';

// One observer for the table, not one observer per cell.
export function useTableTextAlignment(container: HTMLDivElement | null) {
  useEffect(() => {
    if (!container) return;
    let frame = 0;
    const measure = () => {
      frame = 0;
      const elements = container.querySelectorAll<HTMLElement>('tbody p, [data-auto-align]');
      for (const element of elements) {
        const style = getComputedStyle(element);
        const line = parseFloat(style.lineHeight) || parseFloat(style.fontSize) * 1.2;
        const multiline = element.getBoundingClientRect().height > line + 2;
        const value = multiline ? 'true' : 'false';
        if (element.dataset.multiline !== value) element.dataset.multiline = value;
      }
    };
    const schedule = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(measure);
    };
    const resize = new ResizeObserver(schedule);
    resize.observe(container);
    const table = container.querySelector('table');
    if (table) resize.observe(table);
    const changes = new MutationObserver(schedule);
    changes.observe(container, { childList: true, subtree: true, characterData: true });
    schedule();
    return () => {
      cancelAnimationFrame(frame);
      resize.disconnect();
      changes.disconnect();
    };
  }, [container]);
}
