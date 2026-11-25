import 'server-only';
import { Code, ConnectError } from '@connectrpc/connect';
import type { JsonObject } from '@bufbuild/protobuf';
import type { SellerSession } from '@/features/auth/session';
import { importRPCClient } from './import-connect';
import { importSettings, legacySubmission, legacyDeleteImport } from './import-mutation-compat';
async function commandBody(request: Request): Promise<JsonObject> {
  if (!request.body) return {};
  const reader = request.body.getReader(),
    parts: Uint8Array[] = [];
  let length = 0;
  while (true) {
    const { value, done } = await reader.read();
    if (done) break;
    length += value.byteLength;
    if (length > 256 * 1024) {
      await reader.cancel();
      throw new ConnectError('JSON 请求超过256KB', Code.ResourceExhausted);
    }
    parts.push(value);
  }
  if (length === 0) return {};
  try {
    const text = Buffer.concat(parts).toString('utf8'),
      value = JSON.parse(text);
    if (!value || Array.isArray(value) || typeof value !== 'object') throw new Error();
    return value as JsonObject;
  } catch {
    throw new ConnectError('请求必须是有效JSON对象', Code.InvalidArgument);
  }
}
export async function connectImportCommand(
  request: Request,
  url: URL,
  path: string,
  session: SellerSession,
  secret: string,
  signal: AbortSignal
) {
  if (Number(request.headers.get('content-length') || 0) > 256 * 1024)
    throw new ConnectError('JSON 请求超过256KB', Code.ResourceExhausted);
  const parts = path.split('/'),
    id = parts[1];
  const name =
    request.method === 'DELETE'
      ? 'DeleteImport'
      : parts[2] === 'removal-preview'
        ? 'GetImportRemovalPreview'
        : parts[0] === 'jobs'
          ? 'RetryImportJob'
          : parts[2] === 'commit'
            ? 'CommitImport'
            : 'UpdateImportSettings';
  const { client, options } = importRPCClient(name, session, secret, signal);
  const mutation = {
    idempotencyKey: request.headers.get('idempotency-key') || '',
    originalQuery: url.search
  };
  if (name === 'GetImportRemovalPreview') {
    const out = await client.getImportRemovalPreview({ id }, options);
    const counts = [out.products, out.exclusiveProducts, out.sourceRows].map(Number);
    if (counts.some((n) => !Number.isSafeInteger(n) || n < 0))
      throw new Error('Unsafe removal count');
    return {
      status: 200,
      body: { products: counts[0], exclusive_products: counts[1], source_rows: counts[2] }
    };
  }
  if (name === 'DeleteImport')
    return {
      status: 200,
      body: legacyDeleteImport(await client.deleteImport({ id, mutation }, options))
    };
  const body = await commandBody(request);
  const out =
    name === 'CommitImport'
      ? await client.commitImport({ id, mutation, legacyFields: body }, options)
      : name === 'RetryImportJob'
        ? await client.retryImportJob({ id, mutation, legacyFields: body }, options)
        : await client.updateImportSettings(
            { id, mutation, settings: importSettings(body) },
            options
          );
  const result = legacySubmission(out.submission);
  return {
    status: request.method === 'POST' && result.state === 'queued' ? 202 : 200,
    body: result
  };
}
