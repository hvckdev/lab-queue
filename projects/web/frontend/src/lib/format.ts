export const timeZone = Intl.DateTimeFormat().resolvedOptions().timeZone;

export function formatInterval(start: string | Date, end: string | Date, zone = timeZone): string {
  const first = new Date(start);
  const last = new Date(end);
  const day = new Intl.DateTimeFormat('en-CA', { timeZone: zone, year: 'numeric', month: '2-digit', day: '2-digit' });
  const full = new Intl.DateTimeFormat('ru-RU', { timeZone: zone, year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
  const clock = new Intl.DateTimeFormat('ru-RU', { timeZone: zone, hour: '2-digit', minute: '2-digit' });
  return `${full.format(first)} — ${day.format(first) === day.format(last) ? clock.format(last) : full.format(last)}`;
}

export function formatDate(value: string | Date): string {
  return new Intl.DateTimeFormat('ru-RU', { day: 'numeric', month: 'long', year: 'numeric' }).format(new Date(value));
}

export function formatClock(value: string | Date): string {
  return new Intl.DateTimeFormat('ru-RU', { hour: '2-digit', minute: '2-digit' }).format(new Date(value));
}

export function localInput(value: Date): string {
  return new Date(value.getTime() - value.getTimezoneOffset() * 60_000).toISOString().slice(0, 16);
}

export function initials(name: string): string {
  return name.trim().split(/\s+/).slice(0, 2).map((part) => part[0] || '').join('').toUpperCase();
}

export function participants(count: number): string {
  const forms: Record<Intl.LDMLPluralRule, string> = { one: 'участник', few: 'участника', many: 'участников', other: 'участника', zero: 'участников', two: 'участника' };
  return `${count} ${forms[new Intl.PluralRules('ru-RU').select(count)]}`;
}
