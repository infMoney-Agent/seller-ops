import 'server-only';
import { fromJson, toJson, type Message, type JsonObject } from '@bufbuild/protobuf';
import { createClient, Code, ConnectError, type CallOptions } from '@connectrpc/connect';
import { createConnectTransport } from '@connectrpc/connect-node';
import { AuthService } from '@/gen/sellerops/auth/v1/auth_pb';
import { ErrorDetailSchema } from '@/gen/sellerops/common/v1/error_pb';
import { fromLegacyRPCJSON, legacyRPCJSON } from './rpc-compat';
import { authRPCRoutes } from './auth-rpc-routes';
import { commandBody } from '@/features/screening/import-command-connect';
function matchAuth(method: string, path: string) {
  for (const route of authRPCRoutes) {
    if (route.method !== method) continue;
    const keys: string[] = [];
    const match = new RegExp(
      '^' +
        route.path.replace(/\{([^}]+)\}/g, (_, key) => {
          keys.push(key);
          return '([^/]+)';
        }) +
        '$'
    ).exec(path);
    if (match) return { route, args: Object.fromEntries(keys.map((k, i) => [k, match[i + 1]])) };
  }
  return null;
}
export async function authBackendResponse(path: string, init: RequestInit = {}): Promise<Response> {
  const methodName = init.method || 'GET',
    url = new URL(path, 'http://auth.internal'),
    matched = matchAuth(methodName, url.pathname);
  const mode = process.env.AUTH_TRANSPORT || 'connect';
  if (!matched || mode === 'legacy')
    return fetch((process.env.SELLER_API_URL || 'http://127.0.0.1:8080') + path, init);
  if (mode !== 'connect')
    return Response.json({ success: false, message: '认证传输配置无效' }, { status: 503 });
  const headers = new Headers(init.headers),
    origins = (process.env.SELLER_APP_ORIGIN || '')
      .split(',')
      .map((x) => x.trim())
      .filter(Boolean);
  if (!origins.length)
    return Response.json({ success: false, message: '认证服务尚未配置应用来源' }, { status: 503 });
  const write = !['GET', 'HEAD'].includes(methodName);
  if (write && !origins.includes(headers.get('origin') || ''))
    return Response.json({ success: false, message: 'invalid request origin' }, { status: 403 });
  const responseHeaders = new Headers({ 'Cache-Control': 'no-store' });
  try {
    const method = AuthService.methods.find((m) => m.name === matched.route.name);
    if (!method) throw new Error('Missing auth descriptor');
    const schema = method.input.fields.find((f) => f.name === 'body')?.message;
    if (!schema) throw new Error('Missing auth body descriptor');
    const request = new Request('http://auth.internal' + path, {
      method: methodName,
      headers,
      body: init.body,
      signal: init.signal
    });
    const body = matched.route.body ? await commandBody(request, 64 * 1024) : {};
    const q: JsonObject = {},
      extra: JsonObject = {};
    const known = new Set(
      method.input.fields
        .find((f) => f.name === 'query')
        ?.message?.fields.filter((f) => f.fieldKind === 'list')
        .map((f) => f.name)
    );
    for (const key of new Set(url.searchParams.keys())) {
      const values = url.searchParams.getAll(key);
      if (known.has(key)) q[key] = values;
      else extra[key] = { values };
    }
    q.extra = extra;
    let input: Message;
    try {
      input = fromJson(method.input, {
        ...matched.args,
        query: q,
        body:
          schema.typeName === 'google.protobuf.Struct'
            ? body
            : toJson(schema, fromLegacyRPCJSON(schema, body))
      });
    } catch {
      throw new ConnectError('invalid request', Code.InvalidArgument);
    }
    const client = createClient(
      AuthService,
      createConnectTransport({
        baseUrl: process.env.SELLER_API_URL || 'http://127.0.0.1:8080',
        httpVersion: '1.1',
        useBinaryFormat: false,
        defaultTimeoutMs: 30000
      })
    ) as unknown as Record<string, (input: Message, options: CallOptions) => Promise<Message>>;
    const response = (await client[method.localName](input, {
      signal: init.signal || AbortSignal.timeout(30000),
      headers: {
        cookie: headers.get('cookie') || '',
        origin: write ? headers.get('origin') || '' : origins[0]
      },
      onHeader: (h) => {
        for (const value of h.getSetCookie()) responseHeaders.append('Set-Cookie', value);
      }
    })) as Message & { data?: Message; statusCode?: number };
    const status = response.statusCode || 200;
    if (status === 204) return new Response(null, { status, headers: responseHeaders });
    const dataSchema = method.output.fields.find((f) => f.name === 'data')?.message;
    if (!dataSchema || !response.data) throw new Error('Incomplete auth response');
    const data = legacyRPCJSON(dataSchema, response.data);
    return Response.json(matched.route.array ? data.items : data, {
      status,
      headers: responseHeaders
    });
  } catch (error) {
    if (error instanceof ConnectError) {
      for (const value of error.metadata.getSetCookie())
        responseHeaders.append('Set-Cookie', value);
      const detail = error.findDetails(ErrorDetailSchema)[0],
        reason = detail?.reason || '',
        status =
          Number(reason.match(/^HTTP_(\d+)/)?.[1]) ||
          (
            {
              [Code.Unauthenticated]: 401,
              [Code.PermissionDenied]: 403,
              [Code.NotFound]: 404,
              [Code.Aborted]: 409,
              [Code.InvalidArgument]: 400,
              [Code.ResourceExhausted]: 413
            } as Partial<Record<Code, number>>
          )[error.code] ||
          503;
      const body = reason.endsWith('_PLAIN')
        ? { message: error.rawMessage }
        : { success: false, message: error.rawMessage };
      return Response.json(body, { status, headers: responseHeaders });
    }
    return Response.json(
      { success: false, message: '认证服务暂时不可用' },
      { status: 503, headers: responseHeaders }
    );
  }
}
