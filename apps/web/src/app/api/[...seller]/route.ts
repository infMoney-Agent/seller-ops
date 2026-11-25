import { authBackendResponse } from '@/lib/auth-backend';

async function proxy(request: Request) {
  const url = new URL(request.url);
  if (!/^\/api\/(auth|products|users|invitations)(\/|$)/.test(url.pathname)) {
    return new Response(null, { status: 404 });
  }
  const headers = new Headers();
  for (const name of ['cookie', 'origin', 'content-type']) {
    const value = request.headers.get(name);
    if (value) headers.set(name, value);
  }
  let body: Uint8Array<ArrayBuffer> | undefined;
  if (request.body) {
    const reader = request.body.getReader();
    body = new Uint8Array(64 * 1024);
    let length = 0;
    while (true) {
      const { value, done } = await reader.read();
      if (done) break;
      if (value.byteLength > body.length - length) {
        await reader.cancel();
        return Response.json({ message: 'Request too large' }, { status: 413 });
      }
      body.set(value, length);
      length += value.byteLength;
    }
    body = body.subarray(0, length);
  }
  const response = await authBackendResponse(url.pathname + url.search, {
    method: request.method,
    headers,
    body,
    redirect: 'manual',
    cache: 'no-store'
  });
  const outgoing = new Headers({ 'Cache-Control': 'no-store' });
  for (const name of ['content-type', 'location']) {
    const value = response.headers.get(name);
    if (value) outgoing.set(name, value);
  }
  for (const value of response.headers.getSetCookie()) outgoing.append('Set-Cookie', value);
  return new Response(response.body, { status: response.status, headers: outgoing });
}

export { proxy as GET, proxy as POST, proxy as PATCH, proxy as PUT, proxy as DELETE };
