import 'server-only';
import { authBackendResponse } from '@/lib/auth-backend';
import type { SellerSession } from '@/features/auth/session';

export type WorkspaceSessionOutcome =
  | { ok: true; session: SellerSession; durationMs: number }
  | {
      ok: false;
      reason: 'unauthenticated' | 'upstream_error' | 'no_workspace' | 'workspace_switched';
      durationMs: number;
    };

export async function resolveWorkspaceSession(request: Request): Promise<WorkspaceSessionOutcome> {
  const started = performance.now();
  const auth = await authBackendResponse('/api/auth/session', {
    headers: { cookie: request.headers.get('cookie') || '' },
    cache: 'no-store',
    redirect: 'manual',
    signal: AbortSignal.timeout(10000)
  });
  const durationMs = performance.now() - started;
  if (auth.status === 401) {
    return { ok: false, reason: 'unauthenticated', durationMs };
  }
  if (!auth.ok) {
    return { ok: false, reason: 'upstream_error', durationMs };
  }
  const session: SellerSession = await auth.json();
  if (!Number.isSafeInteger(session.id) || session.id <= 0) {
    throw new Error('Invalid session');
  }
  if (
    !Number.isSafeInteger(session.workspaceId) ||
    session.workspaceId <= 0 ||
    !['owner', 'member'].includes(session.role)
  ) {
    return { ok: false, reason: 'no_workspace', durationMs };
  }
  if (
    request.headers.has('x-seller-workspace') &&
    request.headers.get('x-seller-workspace') !== String(session.workspaceId)
  ) {
    return { ok: false, reason: 'workspace_switched', durationMs };
  }
  return { ok: true, session, durationMs };
}
