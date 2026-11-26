import { authBackendResponse } from '@/lib/auth-backend';
import { connectEcom, ecomConnectFailure } from '@/features/screening/ecom-connect';
import { isAllowedAppOrigin } from '@/lib/backend';
import type { SellerSession } from '@/features/auth/session';

const allowed = new Map([
  ['product-research', new Set(['GET categories', 'POST research', 'GET score/:asin'])],
  ['listing', new Set(['GET languages', 'POST generate', 'POST optimize'])],
  ['reviews', new Set(['POST analyze'])],
  [
    'monitor',
    new Set([
      'GET list',
      'POST add',
      'DELETE remove/:asin',
      'GET snapshots/:asin',
      'POST snapshot/:asin'
    ])
  ],
  ['ads', new Set(['POST optimize'])]
]);

async function proxy(request: Request, { params }: { params: Promise<{ path: string[] }> }) {
  const [module, action, asin, ...extra] = (await params).path;
  const signature = request.method + ' ' + action + (asin ? '/:asin' : '');
  if (
    extra.length ||
    !allowed.get(module)?.has(signature) ||
    (asin && !/^[A-Z0-9]{10}$/i.test(asin))
  ) {
    return Response.json({ detail: '电商接口不存在。' }, { status: 404 });
  }
  if (request.method !== 'GET' && !isAllowedAppOrigin(request)) {
    return Response.json({ detail: '请求来源无效。' }, { status: 403 });
  }

  try {
    const auth = await authBackendResponse('/api/auth/session', {
      headers: { cookie: request.headers.get('cookie') || '' },
      cache: 'no-store',
      redirect: 'manual',
      signal: AbortSignal.timeout(10000)
    });
    if (auth.status === 401) {
      return Response.json({ detail: '请登录后继续。' }, { status: 401 });
    }
    if (!auth.ok) throw new Error('Session check failed');
    const session: SellerSession = await auth.json();
    if (!Number.isSafeInteger(session.id) || session.id <= 0) throw new Error('Invalid session');
    if (
      !Number.isSafeInteger(session.workspaceId) ||
      session.workspaceId <= 0 ||
      !['owner', 'member'].includes(session.role)
    ) {
      return Response.json({ detail: '请先通过邀请加入公司。' }, { status: 403 });
    }

    try {
      return Response.json(
        await connectEcom(
          request,
          module + '/' + action + (asin ? '/' + asin.toUpperCase() : ''),
          session.id
        ),
        { headers: { 'Cache-Control': 'no-store' } }
      );
    } catch (error) {
      const failure = ecomConnectFailure(error);
      return Response.json(failure.body, {
        status: failure.status,
        headers: { 'Cache-Control': 'no-store' }
      });
    }
  } catch (error) {
    console.error('E-commerce proxy failed', error);
    return Response.json({ detail: '电商服务暂时不可用。' }, { status: 502 });
  }
}

export { proxy as GET, proxy as POST, proxy as DELETE };
