import { connectEcom, ecomConnectFailure } from '@/features/ecom/ecom-connect';
import { isAllowedAppOrigin } from '@/lib/backend';
import { resolveWorkspaceSession } from '@/lib/workspace-session';

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
    const auth = await resolveWorkspaceSession(request);
    if (!auth.ok) {
      if (auth.reason === 'unauthenticated') {
        return Response.json({ detail: '请登录后继续。' }, { status: 401 });
      }
      if (auth.reason === 'no_workspace') {
        return Response.json({ detail: '请先通过邀请加入公司。' }, { status: 403 });
      }
      if (auth.reason === 'workspace_switched') {
        return Response.json({ detail: '工作区已切换，请刷新页面。' }, { status: 409 });
      }
      throw new Error('Session check failed');
    }

    try {
      return Response.json(
        await connectEcom(
          request,
          module + '/' + action + (asin ? '/' + asin.toUpperCase() : ''),
          auth.session.id
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
