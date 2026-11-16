import { backendURL } from '@/lib/backend';
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
  const url = new URL(request.url);
  const appOrigin = process.env.SELLER_APP_ORIGIN || url.origin;
  if (request.method !== 'GET' && request.headers.get('origin') !== appOrigin) {
    return Response.json({ detail: '请求来源无效。' }, { status: 403 });
  }

  try {
    const auth = await fetch(backendURL('/api/auth/session'), {
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

    let body: Uint8Array<ArrayBuffer> | undefined;
    if (request.body) {
      const reader = request.body.getReader();
      const buffer = new Uint8Array(64 * 1024);
      let length = 0;
      while (true) {
        const { value, done } = await reader.read();
        if (done) break;
        if (value.byteLength > buffer.length - length) {
          await reader.cancel();
          return Response.json({ detail: '请求内容过大。' }, { status: 413 });
        }
        buffer.set(value, length);
        length += value.byteLength;
      }
      body = buffer.subarray(0, length);
    }

    const base = process.env.ECOM_API_URL || 'http://127.0.0.1:18080';
    const target = new URL(
      '/api/v1/' + module + '/' + action + (asin ? '/' + asin.toUpperCase() : ''),
      base
    );
    target.search = url.search;
    const response = await fetch(target, {
      method: request.method,
      headers: {
        'content-type': 'application/json',
        'x-seller-user': String(session.id)
      },
      body,
      cache: 'no-store',
      redirect: 'error',
      signal: AbortSignal.timeout(120000)
    });
    if (response.status >= 500) {
      console.error('E-commerce request failed', module, action, response.status);
      return Response.json({ detail: '操作失败，请检查服务配置或重试。' }, { status: 502 });
    }
    return new Response(response.body, {
      status: response.status,
      headers: { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' }
    });
  } catch (error) {
    console.error('E-commerce proxy failed', error);
    return Response.json({ detail: '电商服务暂时不可用。' }, { status: 502 });
  }
}

export { proxy as GET, proxy as POST, proxy as DELETE };
