import countries from './country-codes.json';
const supported = new Set(countries);
const aliases: Record<string, string> = {
  UK: 'GB',
  USA: 'US',
  美国: 'US',
  中国: 'CN',
  中国大陆: 'CN',
  中国香港: 'HK',
  香港: 'HK',
  中国台湾: 'TW',
  台湾: 'TW',
  英国: 'GB',
  韩国: 'KR'
};
let names: Map<string, string> | undefined;
export function countryCode(region: string | null | undefined): string | null {
  const raw = region?.trim();
  if (!raw) return null;
  const code = raw.toUpperCase();
  if (supported.has(code)) return code;
  if (aliases[code]) return aliases[code];
  if (!names) {
    names = new Map();
    for (const locale of ['zh-CN', 'en']) {
      const display = new Intl.DisplayNames([locale], { type: 'region' });
      for (const id of countries) {
        const name = display.of(id);
        if (name) names.set(name.toLowerCase(), id);
      }
    }
  }
  return names.get(raw.toLowerCase()) ?? null;
}
function calendarDay(value: string): number | null {
  const m = /^(\d{4})[-/.](\d{1,2})[-/.](\d{1,2})(?:$|[ T])/.exec(value.trim());
  if (!m) return null;
  const year = Number(m[1]),
    month = Number(m[2]),
    day = Number(m[3]);
  if (year < 1000 || month < 1 || month > 12 || day < 1 || day > 31) return null;
  const date = new Date(Date.UTC(year, month - 1, day));
  if (
    date.getUTCFullYear() !== year ||
    date.getUTCMonth() !== month - 1 ||
    date.getUTCDate() !== day
  )
    return null;
  return date.getTime() / 86400000;
}
export function listingAge(value: string | null | undefined, today: string): number | null {
  if (!value || !today) return null;
  const listed = calendarDay(value),
    now = calendarDay(today);
  if (listed == null || now == null || listed > now) return null;
  return now - listed;
}
export function shanghaiToday(): string {
  return new Intl.DateTimeFormat('en-CA', {
    timeZone: 'Asia/Shanghai',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit'
  }).format(new Date());
}
