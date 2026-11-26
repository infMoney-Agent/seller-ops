import 'server-only';
import { createHmac } from 'node:crypto';
import { Code, ConnectError, type CallOptions } from '@connectrpc/connect';
import { fromJson, toJson, type Message, type JsonObject } from '@bufbuild/protobuf';
import { EcomService } from '@/gen/sellerops/ecom/v1/ecom_pb';
import { ErrorDetailSchema } from '@/gen/sellerops/common/v1/error_pb';
import { getInternalConnectClient } from '@/lib/connect-transport';
import { fromLegacyRPCJSON, legacyRPCJSON } from '@/lib/rpc-compat';
import { commandBody } from '@/features/screening/import-command-connect';
import { ecomRPCRoutes } from './ecom-rpc-routes';

export async function connectEcom(request: Request, path: string, user: number) {
  const matched = ecomRPCRoutes
    .map((route) => ({
      route,
      match: new RegExp('^' + route.path.replace('{asin}', '([A-Z0-9]{10})') + '$').exec(path)
    }))
    .find((x) => x.match && x.route.method === request.method);
  if (!matched?.match) throw new Error('No RPC route');
  const method = EcomService.methods.find((m) => m.name === matched.route.name);
  if (!method) throw new Error('Missing RPC descriptor');
  const url = new URL(request.url),
    q: JsonObject = {},
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
  if (Number(request.headers.get('content-length') || 0) > 64 * 1024)
    throw new ConnectError('请求内容过大', Code.ResourceExhausted);
  const body = matched.route.body ? await commandBody(request, 64 * 1024) : {};
  const schema = method.input.fields.find((f) => f.name === 'body')?.message;
  if (!schema) throw new Error('Missing body schema');
  let input: Message;
  try {
    input = fromJson(method.input, {
      asin: matched.match[1] || '',
      query: q,
      body:
        schema.typeName === 'google.protobuf.Struct'
          ? body
          : toJson(schema, fromLegacyRPCJSON(schema, body))
    });
  } catch {
    throw new ConnectError('请求字段无效', Code.InvalidArgument);
  }
  const secret = process.env.ECOM_INTERNAL_SECRET || '';
  if (secret.length < 32) throw new Error('Missing ecom secret');
  const stamp = String(Math.floor(Date.now() / 1000)),
    procedure = '/' + EcomService.typeName + '/' + method.name;
  const signature = createHmac('sha256', secret)
    .update([stamp, 'POST', procedure, String(user)].join('\n'))
    .digest('hex');
  const client = getInternalConnectClient(
    EcomService,
    process.env.ECOM_API_URL || 'http://127.0.0.1:18080',
    120000
  ) as unknown as Record<string, (input: Message, options: CallOptions) => Promise<Message>>;
  const response = await client[method.localName](input, {
    signal: AbortSignal.timeout(120000),
    headers: { 'x-seller-user': String(user), 'x-ecom-time': stamp, 'x-ecom-signature': signature }
  });
  const schemaOut = method.output.fields.find((f) => f.name === 'data')?.message,
    data = (response as Message & { data?: Message }).data;
  if (!schemaOut || !data) throw new Error('Incomplete RPC response');
  return legacyRPCJSON(schemaOut, data);
}

export function ecomConnectFailure(error: unknown) {
  if (!(error instanceof ConnectError))
    return { status: 502, body: { detail: '电商服务暂时不可用。' } };
  const detail = error.findDetails(ErrorDetailSchema)[0];
  const raw = Number(detail?.reason.match(/^HTTP_(\d+)$/)?.[1]);
  const status =
    raw ||
    (
      {
        [Code.Unauthenticated]: 401,
        [Code.PermissionDenied]: 403,
        [Code.NotFound]: 404,
        [Code.Aborted]: 409,
        [Code.InvalidArgument]: 422,
        [Code.ResourceExhausted]: 413
      } as Partial<Record<Code, number>>
    )[error.code] ||
    502;
  if (status >= 500) return { status: 502, body: { detail: '操作失败，请检查服务配置或重试。' } };
  return { status, body: { detail: error.rawMessage } };
}
