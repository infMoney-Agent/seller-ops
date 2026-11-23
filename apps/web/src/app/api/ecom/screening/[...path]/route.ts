import { createHmac } from 'node:crypto';
import { acceptsGzip, serverTiming } from '@/lib/http-performance';
import { backendURL } from '@/lib/backend';
import type { SellerSession } from '@/features/auth/session';

export const runtime = 'nodejs';
const uuid = '[0-9a-fA-F-]{36}';
const routes = [
  [
    'GET',
    'parents/monthly|summary|months|products|imports|policies|review-batches|review-workspace|review-categories|profit/templates|profit/products'
  ],
  [
    'POST',
    'imports|policies|screen-runs|review-batches/preview|profit/calculate|profit/templates|profit/batches|exports'
  ],
  ['GET', `products/${uuid}(/(family|observations|monthly|history|references|profit-scenarios))?`],
  ['GET', `products/${uuid}/observations/${uuid}`],
  ['GET', `imports/${uuid}(/(preview|errors|source|removal-preview))?`],
  [
    'GET',
    `screen-runs/${uuid}|review-batches/${uuid}|jobs/${uuid}|exports/${uuid}/download|groups/[a-f0-9]{64}/products`
  ],
  ['PATCH', `products/${uuid}/(review|category|parameters)`],
  ['PUT', `imports/${uuid}/mapping`],
  ['DELETE', `imports/${uuid}`],
  [
    'POST',
    `imports/${uuid}/commit|policies/${uuid}/(test|activate)|review-batches/${uuid}/(apply|undo)|products/${uuid}/profit-scenarios|jobs/${uuid}/retry`
  ]
] as const;

async function proxy(request: Request, { params }: { params: Promise<{ path: string[] }> }) {
  const started = performance.now();
  const path = (await params).path.join('/');
  if (
    !routes.some(
      ([method, pattern]) => method === request.method && new RegExp(`^(?:${pattern})$`).test(path)
    )
  ) {
    return Response.json({ message: '初筛接口不存在。' }, { status: 404 });
  }
  const url = new URL(request.url);
  const write = !['GET', 'HEAD'].includes(request.method);
  if (write && request.headers.get('origin') !== (process.env.SELLER_APP_ORIGIN || url.origin)) {
    return Response.json({ message: '请求来源无效。' }, { status: 403 });
  }
  const secret = process.env.SCREENING_INTERNAL_SECRET || '';
  if (secret.length < 32) return Response.json({ message: '初筛服务尚未配置。' }, { status: 503 });
  let oversized = false;
  try {
    const authStarted = performance.now();
    const auth = await fetch(backendURL('/api/auth/session'), {
      headers: { cookie: request.headers.get('cookie') || '' },
      cache: 'no-store',
      redirect: 'manual',
      signal: AbortSignal.timeout(10000)
    });
    if (!auth.ok)
      return Response.json(
        { message: auth.status === 401 ? '请先登录。' : '身份服务暂不可用。' },
        { status: auth.status === 401 ? 401 : 502 }
      );
    const session: SellerSession = await auth.json();
    const authDuration = performance.now() - authStarted;
    if (
      !Number.isSafeInteger(session.id) ||
      session.id <= 0 ||
      !Number.isSafeInteger(session.workspaceId) ||
      session.workspaceId <= 0 ||
      !['owner', 'member'].includes(session.role)
    ) {
      return Response.json({ message: '账号尚未绑定租户，请联系管理员。' }, { status: 403 });
    }
    if (
      request.headers.has('x-seller-workspace') &&
      request.headers.get('x-seller-workspace') !== String(session.workspaceId)
    ) {
      return Response.json({ message: '工作区已切换，请刷新页面。' }, { status: 409 });
    }
    const stamp = String(Math.floor(Date.now() / 1000));
    const targetPath = '/api/v1/screening/' + path + url.search;
    const signature = createHmac('sha256', secret)
      .update(
        [
          stamp,
          request.method,
          targetPath,
          String(session.id),
          String(session.workspaceId),
          session.role
        ].join('\n')
      )
      .digest('hex');
    const headers = new Headers({
      'x-seller-user': String(session.id),
      'x-seller-workspace': String(session.workspaceId),
      'x-seller-role': session.role,
      'x-screening-time': stamp,
      'x-screening-signature': signature
    });
    const key = request.headers.get('idempotency-key');
    if (key) headers.set('idempotency-key', key);
    const contentType = request.headers.get('content-type');
    if (contentType) headers.set('content-type', contentType);
    const upload = request.method === 'POST' && path === 'imports';
    const limit = upload ? 30 * 1024 * 1024 + 64 * 1024 : 256 * 1024;
    if (Number(request.headers.get('content-length') || 0) > limit)
      return Response.json({ message: '请求内容过大。' }, { status: 413 });
    let length = 0;
    const body = request.body?.pipeThrough(
      new TransformStream<Uint8Array, Uint8Array>({
        transform(chunk, controller) {
          length += chunk.byteLength;
          if (length > limit) {
            oversized = true;
            throw new Error('Payload too large');
          }
          controller.enqueue(chunk);
        }
      })
    );
    const init: RequestInit & { duplex?: string } = {
      method: request.method,
      headers,
      body,
      duplex: 'half',
      cache: 'no-store',
      redirect: 'error',
      signal: write
        ? AbortSignal.timeout(upload ? 180000 : 120000)
        : AbortSignal.any([request.signal, AbortSignal.timeout(120000)])
    };
    const apiStarted = performance.now();
    const response = await fetch(
      new URL(targetPath, process.env.SCREENING_API_URL || 'http://127.0.0.1:18090'),
      init
    );
    const apiDuration = performance.now() - apiStarted;
    const responseHeaders = new Headers({
      'Cache-Control': 'no-store',
      'Server-Timing': serverTiming(authDuration, apiDuration, performance.now() - started)
    });
    for (const name of ['content-type', 'content-disposition']) {
      const value = response.headers.get(name);
      if (value) responseHeaders.set(name, value);
    }
    let responseBody = response.body;
    const compressibleJSON =
      ['products', 'profit/products', 'review-categories', 'review-workspace'].includes(path) ||
      /^products\/[^/]+\/family$/.test(path);
    if (
      !write &&
      compressibleJSON &&
      response.ok &&
      responseBody &&
      response.headers.get('content-type')?.includes('application/json')
    ) {
      responseHeaders.set('Vary', 'Accept-Encoding');
      if (acceptsGzip(request.headers.get('accept-encoding'))) {
        responseHeaders.set('Content-Encoding', 'gzip');
        responseBody = responseBody.pipeThrough(new CompressionStream('gzip'));
      }
    }
    return new Response(responseBody, { status: response.status, headers: responseHeaders });
  } catch (error) {
    console.error(
      'Screening proxy failed',
      error instanceof Error ? error.message : 'Unknown error'
    );
    return Response.json(
      { message: oversized ? '请求内容过大。' : '初筛服务暂时不可用。' },
      { status: oversized ? 413 : 502 }
    );
  }
}
export { proxy as GET, proxy as POST, proxy as PUT, proxy as PATCH, proxy as DELETE };
