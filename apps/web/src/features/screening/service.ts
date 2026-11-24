import { v4 as randomUUID } from 'uuid';
const base = '/api/ecom/screening';
export function workspaceHeader(): Record<string, string> {
  const workspace =
    document.querySelector<HTMLElement>('[data-seller-workspace]')?.dataset.sellerWorkspace;
  return workspace ? { 'X-Seller-Workspace': workspace } : {};
}
export async function request<T>(
  path: string,
  method = 'GET',
  body?: unknown,
  signal?: AbortSignal,
  priority: RequestPriority = 'auto',
  writeOptions?: { idempotencyKey?: string; keepalive?: boolean; workspaceId?: string }
): Promise<T> {
  const upload = body instanceof FormData;
  const options: RequestInit = {
    method,
    signal,
    priority,
    ...(writeOptions?.keepalive ? { keepalive: true } : {}),
    cache: 'no-store',
    headers: {
      ...workspaceHeader(),
      ...(writeOptions?.workspaceId !== undefined
        ? { 'X-Seller-Workspace': writeOptions.workspaceId }
        : {}),
      ...(method !== 'GET'
        ? { 'Idempotency-Key': writeOptions?.idempotencyKey || randomUUID() }
        : {}),
      ...(body && !upload ? { 'Content-Type': 'application/json' } : {})
    }
  };
  if (method !== 'GET' && body !== undefined) options.body = upload ? body : JSON.stringify(body);
  const response = await fetch(base + path, options);
  if (!response.ok) {
    const error = (await response.json().catch(() => null)) as {
      message?: string;
      detail?: string;
    } | null;
    throw new Error(error?.message || error?.detail || `操作失败（${response.status}）`);
  }
  return response.json() as Promise<T>;
}
export async function download(path: string, filename: string) {
  const response = await fetch(base + path, { headers: workspaceHeader(), cache: 'no-store' });
  if (!response.ok) {
    const error = (await response.json().catch(() => null)) as { message?: string } | null;
    throw new Error(error?.message || '下载失败');
  }
  const url = URL.createObjectURL(await response.blob());
  const link = document.createElement('a');
  link.href = url;
  link.download = filename;
  link.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
