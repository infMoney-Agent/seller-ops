import { useQuery } from '@tanstack/react-query';
import { useSellerSession } from '@/features/auth/session';
import { request } from './service';
import type { Job } from './types';

export function useScreeningQuery<T>(path: string | null, poll = false) {
  const session = useSellerSession();
  return useQuery({
    queryKey: ['screening', session.workspaceId, path],
    queryFn: () => request<T>(path!),
    enabled: Boolean(path),
    refetchInterval: poll ? 2000 : false
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
