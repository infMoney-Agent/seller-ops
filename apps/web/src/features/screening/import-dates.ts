export function defaultObservationDate() {
  const parts = new Intl.DateTimeFormat('en', {
    timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit'
  }).formatToParts(new Date());
  const value = (type: string) => parts.find((part) => part.type === type)?.value;
  return `${value('year')}-${value('month')}-${value('day')}`;
}

export function sourcePeriodFromFilename(name: string) {
  const months = new Set(
    [...name.matchAll(/(?:^|[^0-9])((?:19|20)\d{2})[._-](0[1-9]|1[0-2])(?=[^0-9]|$)/g)]
      .map((match) => `${match[1]}-${match[2]}`)
  );
  return months.size === 1 ? [...months][0] : '';
}
