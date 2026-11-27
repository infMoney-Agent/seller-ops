import 'server-only';
import { createHmac } from 'node:crypto';
import { Code, ConnectError, type CallOptions } from '@connectrpc/connect';
import { fromJson, toJson, type Message, type JsonObject } from '@bufbuild/protobuf';
import { ScreeningService } from '@/gen/sellerops/screening/v1/screening_pb';
import { getInternalConnectClient } from '@/lib/connect-transport';
import { fromLegacyRPCJSON, legacyRPCJSON } from '@/lib/rpc-compat';
import type { SellerSession } from '@/features/auth/session';
import { commandBody } from './import-command-connect';
import { screeningRPCRoutes } from './screening-rpc-routes';
export function screeningRPCRoute(method: string, path: string) {
  for (const route of screeningRPCRoutes) {
    if (route.method !== method) continue;
    const keys: string[] = [];
    const pattern = route.path.replace(/\{([^}]+)\}/g, (_, key) => {
      keys.push(key);
      return '([^/]+)';
    });
    const match = new RegExp('^' + pattern + '$').exec(path);
    if (match) return { route, args: Object.fromEntries(keys.map((k, i) => [k, match[i + 1]])) };
  }
  return null;
}
export async function connectScreening(
  request: Request,
  url: URL,
  path: string,
  session: SellerSession,
  secret: string,
  signal: AbortSignal
) {
  const matched = screeningRPCRoute(request.method, path);
  if (!matched) throw new Error('RPC route missing');
  const method = ScreeningService.methods.find((m) => m.name === matched.route.name);
  if (!method || method.methodKind !== 'unary') throw new Error('RPC descriptor missing');
  const queryField = method.input.fields.find((f) => f.name === 'query');
  const known = new Set(
    queryField?.message?.fields.filter((f) => f.name !== 'extra').map((f) => f.name)
  );
  const query: JsonObject = {},
    extra: JsonObject = {};
  for (const key of new Set(url.searchParams.keys())) {
    const values = url.searchParams.getAll(key);
    if (known.has(key)) query[key] = values;
    else extra[key] = { values };
  }
  query.extra = extra;
  const body = ['POST', 'PUT', 'PATCH'].includes(request.method) ? await commandBody(request) : {};
  const bodySchema = method.input.fields.find((f) => f.name === 'body')?.message;
  if (!bodySchema) throw new Error('RPC body descriptor missing');
  const payload: JsonObject = {
    query,
    mutation: {
      idempotencyKey: request.headers.get('idempotency-key') || '',
      originalQuery: url.search
    },
    ...matched.args
  };
  let input: Message;
  try {
    payload.body =
      bodySchema.typeName === 'google.protobuf.Struct'
        ? body
        : toJson(bodySchema, fromLegacyRPCJSON(bodySchema, body));
    input = fromJson(method.input, payload);
  } catch {
    throw new ConnectError('请求字段结构无效', Code.InvalidArgument);
  }
  const procedure = '/' + ScreeningService.typeName + '/' + method.name,
    stamp = String(Math.floor(Date.now() / 1000));
  const signature = createHmac('sha256', secret)
    .update(
      [
        stamp,
        'POST',
        procedure,
        String(session.id),
        String(session.workspaceId),
        session.role
      ].join('\n')
    )
    .digest('hex');
  const client = getInternalConnectClient(
    ScreeningService,
    process.env.SCREENING_API_URL || 'http://127.0.0.1:18090',
    120000
  ) as unknown as Record<string, (input: Message, options: CallOptions) => Promise<Message>>;
  const response = await client[method.localName](input, {
    signal,
    headers: {
      'x-seller-user': String(session.id),
      'x-seller-workspace': String(session.workspaceId),
      'x-seller-role': session.role,
      'x-screening-time': stamp,
      'x-screening-signature': signature
    }
  });
  const dataField = method.output.fields.find((f) => f.name === 'data');
  if (!dataField?.message) throw new Error('RPC response descriptor missing');
  const data = (response as Message & { data?: Message }).data;
  if (!data) throw new Error('RPC response missing');
  const result = legacyRPCJSON(dataField.message, data);
  return {
    status: request.method === 'POST' && result.job_id && result.state === 'queued' ? 202 : 200,
    body: result
  };
}
