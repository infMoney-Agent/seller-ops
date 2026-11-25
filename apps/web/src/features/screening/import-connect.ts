import 'server-only';
import { createHmac, randomUUID } from 'node:crypto';
import { Code, ConnectError, createClient } from '@connectrpc/connect';
import { createConnectTransport } from '@connectrpc/connect-node';
import { ScreeningImportService } from '@/gen/sellerops/screening/v1/import_pb';
import { ErrorDetailSchema } from '@/gen/sellerops/common/v1/error_pb';
import type { SellerSession } from '@/features/auth/session';
import {
  importPagination,
  legacyImportList,
  legacyImportDetail,
  legacyJob
} from './import-list-compat';
export async function connectImportRead(
  url: URL,
  path: string,
  session: SellerSession,
  secret: string,
  signal: AbortSignal
) {
  const parts = path.split('/');
  const name =
    path === 'imports'
      ? 'ListImports'
      : parts[0] === 'jobs'
        ? 'GetJob'
        : parts[2] === 'preview'
          ? 'ListImportRows'
          : 'GetImport';
  const procedure = '/' + ScreeningImportService.typeName + '/' + name;
  const stamp = String(Math.floor(Date.now() / 1000));
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
  const transport = createConnectTransport({
    baseUrl: process.env.SCREENING_API_URL || 'http://127.0.0.1:18090',
    httpVersion: '1.1',
    useBinaryFormat: false,
    defaultTimeoutMs: 120000
  });
  const client = createClient(ScreeningImportService, transport);
  const options = {
    signal,
    headers: {
      'x-seller-user': String(session.id),
      'x-seller-workspace': String(session.workspaceId),
      'x-seller-role': session.role,
      'x-screening-time': stamp,
      'x-screening-signature': signature
    }
  };
  if (name === 'ListImports')
    return legacyImportList(
      await client.listImports({ pagination: importPagination(url.searchParams) }, options)
    );
  if (name === 'GetImport')
    return legacyImportDetail(await client.getImport({ id: parts[1] }, options));
  if (name === 'ListImportRows')
    return legacyImportList(
      await client.listImportRows(
        { importId: parts[1], pagination: importPagination(url.searchParams) },
        options
      )
    );
  return legacyJob(await client.getJob({ id: parts[1] }, options));
}
export function importConnectFailure(error: unknown): {
  status: number;
  body: Record<string, unknown>;
} {
  if (error instanceof RangeError)
    return {
      status: 422,
      body: { code: 'Unprocessable Entity', message: error.message, request_id: randomUUID() }
    };
  if (!(error instanceof ConnectError))
    return { status: 502, body: { message: '初筛服务暂时不可用。' } };
  const status = (
    {
      [Code.Unauthenticated]: 401,
      [Code.PermissionDenied]: 403,
      [Code.NotFound]: 404,
      [Code.Aborted]: 409,
      [Code.InvalidArgument]: 422,
      [Code.Internal]: 500
    } as Partial<Record<Code, number>>
  )[error.code];
  if (!status) return { status: 502, body: { message: '初筛服务暂时不可用。' } };
  const names: Record<number, string> = {
    401: 'Unauthorized',
    403: 'Forbidden',
    404: 'Not Found',
    409: 'Conflict',
    422: 'Unprocessable Entity',
    500: 'Internal Server Error'
  };
  const detail = error.findDetails(ErrorDetailSchema)[0];
  return {
    status,
    body: {
      code: names[status],
      message: error.rawMessage,
      request_id: detail?.requestId || randomUUID()
    }
  };
}
