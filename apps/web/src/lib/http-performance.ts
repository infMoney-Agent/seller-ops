// HTTP compression is negotiated per request; never share authenticated responses.
export function acceptsGzip(header: string | null): boolean {
  const encodings = (header || '').split(',').map((part) => {
    const [name, ...params] = part.trim().toLowerCase().split(';');
    const quality = params.find((value) => value.trim().startsWith('q='));
    return { name, quality: quality ? Number(quality.trim().slice(2)) : 1 };
  });
  const gzip = encodings.find((value) => value.name === 'gzip');
  const accepted = gzip || encodings.find((value) => value.name === '*');
  return Boolean(
    accepted && Number.isFinite(accepted.quality) && accepted.quality > 0 && accepted.quality <= 1
  );
}
export function serverTiming(auth: number, api: number, total: number): string {
  return (
    'auth;dur=' +
    auth.toFixed(1) +
    ', api;dur=' +
    api.toFixed(1) +
    ', proxy;dur=' +
    total.toFixed(1)
  );
}
