import { createHmac } from 'node:crypto';
import { acceptsGzip, serverTiming } from '@/lib/http-performance';
import { isAllowedAppOrigin } from '@/lib/backend';
import { resolveWorkspaceSession } from '@/lib/workspace-session';
import { connectImportRead, importConnectFailure } from '@/features/screening/import-connect';
import { connectScreening, screeningRPCRoute } from '@/features/screening/screening-connect';
import { connectImportCommand } from '@/features/screening/import-command-connect';

export const runtime = 'nodejs';
const uuid = '[0-9a-fA-F-]{36}';
const routes = [
  [
    'GET',
    'parents/monthly|summary|months|trademark-options|products|imports|policies|review-batches|review-workspace|review-categories|profit/templates|profit/products'
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
  if (write && !isAllowedAppOrigin(request)) {
    return Response.json({ message: '请求来源无效。' }, { status: 403 });
  }
  const secret = process.env.SCREENING_INTERNAL_SECRET || '';
  if (secret.length < 32) return Response.json({ message: '初筛服务尚未配置。' }, { status: 503 });
  let oversized = false;
  try {
    const authOutcome = await resolveWorkspaceSession(request);
    if (!authOutcome.ok) {
      if (authOutcome.reason === 'unauthenticated') {
        return Response.json({ message: '请先登录。' }, { status: 401 });
      }
      if (authOutcome.reason === 'no_workspace') {
        return Response.json({ message: '账号尚未绑定租户，请联系管理员。' }, { status: 403 });
      }
      if (authOutcome.reason === 'workspace_switched') {
        return Response.json({ message: '工作区已切换，请刷新页面。' }, { status: 409 });
      }
      return Response.json({ message: '身份服务暂不可用。' }, { status: 502 });
    }
    const { session, durationMs: authDuration } = authOutcome;
    if (screeningRPCRoute(request.method, path)) {
      const apiStarted = performance.now();
      const responseHeaders = () => ({
        'Cache-Control': 'no-store',
        'Server-Timing': serverTiming(
          authDuration,
          performance.now() - apiStarted,
          performance.now() - started
        )
      });
      try {
        const result = await connectScreening(
          request,
          url,
          path,
          session,
          secret,
          write
            ? AbortSignal.timeout(120000)
            : AbortSignal.any([request.signal, AbortSignal.timeout(120000)])
        );
        return screeningRPCResponse(request, path, result.body, result.status, responseHeaders());
      } catch (error) {
        const failure = importConnectFailure(error);
        return Response.json(failure.body, {
          status: failure.status,
          headers: responseHeaders()
        });
      }
    }
    const importRead = new RegExp(`^(?:imports/${uuid}(?:/preview)?|jobs/${uuid})$`).test(path);
    if (request.method === 'GET' && (path === 'imports' || importRead)) {
      const apiStarted = performance.now();
      const responseHeaders = () => ({
        'Cache-Control': 'no-store',
        'Server-Timing': serverTiming(
          authDuration,
          performance.now() - apiStarted,
          performance.now() - started
        )
      });
      try {
        const result = await connectImportRead(
          url,
          path,
          session,
          secret,
          AbortSignal.any([request.signal, AbortSignal.timeout(120000)])
        );
        return Response.json(result, { headers: responseHeaders() });
      } catch (error) {
        const failure = importConnectFailure(error);
        return Response.json(failure.body, {
          status: failure.status,
          headers: responseHeaders()
        });
      }
    }
    const importCommand =
      (request.method === 'DELETE' && new RegExp(`^imports/${uuid}$`).test(path)) ||
      new RegExp(
        `^(?:imports/${uuid}/(?:commit|mapping|removal-preview)|jobs/${uuid}/retry)$`
      ).test(path);
    if (importCommand) {
      const apiStarted = performance.now();
      const responseHeaders = () => ({
        'Cache-Control': 'no-store',
        'Server-Timing': serverTiming(
          authDuration,
          performance.now() - apiStarted,
          performance.now() - started
        )
      });
      try {
        const result = await connectImportCommand(
          request,
          url,
          path,
          session,
          secret,
          write
            ? AbortSignal.timeout(120000)
            : AbortSignal.any([request.signal, AbortSignal.timeout(120000)])
        );
        return Response.json(result.body, { status: result.status, headers: responseHeaders() });
      } catch (error) {
        const failure = importConnectFailure(error);
        return Response.json(failure.body, {
          status: failure.status,
          headers: responseHeaders()
        });
      }
    }
    const upload = request.method === 'POST' && path === 'imports';
    const download =
      request.method === 'GET' &&
      new RegExp(`^(?:imports/${uuid}/(?:source|errors)|exports/${uuid}/download)$`).test(path);
    if (!upload && !download) {
      return Response.json({ message: '初筛接口不存在。' }, { status: 404 });
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
    return new Response(response.body, { status: response.status, headers: responseHeaders });
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

function screeningRPCResponse(
  request: Request,
  path: string,
  body: unknown,
  status: number,
  headers: Record<string, string>
) {
  const response = Response.json(body, { status, headers });
  const compressible =
    ['products', 'profit/products', 'review-categories', 'review-workspace'].includes(path) ||
    /^products\/[^/]+\/family$/.test(path);
  if (request.method === 'GET' && compressible && response.body) {
    const h = new Headers(response.headers);
    h.set('Vary', 'Accept-Encoding');
    if (acceptsGzip(request.headers.get('accept-encoding'))) {
      h.set('Content-Encoding', 'gzip');
      return new Response(response.body.pipeThrough(new CompressionStream('gzip')), {
        status,
        headers: h
      });
    }
    return new Response(response.body, { status, headers: h });
  }
  return response;
}
