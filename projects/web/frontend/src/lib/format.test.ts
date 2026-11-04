import { describe, expect, it } from 'vitest';
import { formatInterval, initials, participants } from './format';

describe('displayed calendar intervals', () => {
  it('shows one date for a same-day interval', () => {
    const text = formatInterval('2026-10-28T10:00:00Z', '2026-10-28T12:30:00Z', 'Europe/Moscow');
    expect(text).toMatch(/28 окт.*13:00 — 15:30$/);
    expect(text.match(/2026/g)).toHaveLength(1);
  });
  it('preserves both dates when crossing local midnight', () => {
    expect(formatInterval('2026-10-28T20:30:00Z', '2026-10-28T21:30:00Z', 'Europe/Moscow')).toMatch(/28 окт.*23:30 — 29 окт.*00:30/);
  });
  it('compares local dates rather than UTC dates', () => {
    const text = formatInterval('2026-10-28T23:30:00Z', '2026-10-29T00:30:00Z', 'Europe/Moscow');
    expect(text).toMatch(/29 окт.*02:30 — 03:30$/);
    expect(text.match(/2026/g)).toHaveLength(1);
  });
  it('keeps the years across New Year', () => {
    expect(formatInterval('2026-12-31T20:00:00Z', '2027-01-01T10:00:00Z', 'Europe/Moscow')).toMatch(/31 дек.*2026.*23:00 — 1 янв.*2027.*13:00/);
  });
});

it('uses Russian participant forms and safe initials', () => {
  expect([1, 2, 5, 11, 21].map(participants)).toEqual(['1 участник', '2 участника', '5 участников', '11 участников', '21 участник']);
  expect(initials('  Мария  Петрова  ')).toBe('МП');
  expect(initials('')).toBe('');
});
