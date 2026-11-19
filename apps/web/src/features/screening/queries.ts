import { useQuery } from '@tanstack/react-query';
import { useSellerSession } from '@/features/auth/session';
import { request } from './service';
import type { Job } from './types';

export function useScreeningQuery<T>(
  path: string | null,
  poll: boolean | ((data: T | undefined) => boolean) = false
) {
  const session = useSellerSession();
  return useQuery({
    queryKey: ['screening', session.workspaceId, path],
    queryFn: () => request<T>(path!),
    enabled: Boolean(path),
    refetchInterval: (query) => {
      const running = typeof poll === 'function' ? poll(query.state.data) : poll;
      if (!running) return false;
      if (typeof poll !== 'function') return 2000;
      // Long-running queries back off by successful polling count, capped at 10s.
      return Math.min(10000, 2000 * Math.max(1, Math.floor(query.state.dataUpdateCount / 5)));
    }
  });
}
export function useJob(id: string | null) {
  const session = useSellerSession();
  return useQuery({
    queryKey: ['screening-job', session.workspaceId, id],
    queryFn: () => request<Job>('/jobs/' + id),
    enabled: Boolean(id),
    refetchInterval: (query) =>
      ['queued', 'running'].includes(query.state.data?.state || 'queued') ? 2000 : false
  });
}
