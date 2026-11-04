import { act, fireEvent, render, screen, within } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { useQueueDisplay } from '../hooks/useQueueDisplay';
import type { Entry, QueueSnapshot } from '../types';
import { ProjectorScreen } from './ProjectorScreen';

vi.mock('../hooks/useQueueDisplay', () => ({ useQueueDisplay: vi.fn() }));

const now = new Date('2026-11-04T10:00:00Z');
function entry(id: number, called = false): Entry {
  return { id, session_id: 22, student_id: id + 100, student_name: called ? 'Вызванный студент' : `Ожидающий ${id}`, ticket_number: id + 10, status: called ? 'called' : 'waiting', position: id, created_at: now.toISOString() };
}
function snapshot(count = 7): QueueSnapshot {
  return { session: { id: 22, teacher_id: 1, subject_id: 1, subject_name: 'Программирование', group_id: 1, group_name: 'ИВТ-101', lab_title: 'Лабораторная работа', starts_at: '2026-11-04T09:00:00Z', ends_at: '2026-11-04T12:00:00Z', registration_closed: false, registration_open: true, finished: false, active_count: count + 1 }, entries: [entry(1, true), ...Array.from({ length: count }, (_, index) => entry(index + 2))], my_entry: null, next_position: count + 2 };
}
function state(overrides: Partial<ReturnType<typeof useQueueDisplay>> = {}): ReturnType<typeof useQueueDisplay> {
  return { snapshot: snapshot(), loading: false, error: null, lastUpdated: now, access: 'allowed', refresh: vi.fn(), ...overrides };
}

beforeEach(() => {
  vi.useFakeTimers();
  vi.setSystemTime(now);
  vi.mocked(useQueueDisplay).mockReturnValue(state());
});
afterEach(() => {
  vi.useRealTimers();
  delete (document.documentElement as Partial<HTMLElement>).requestFullscreen;
});

describe('ProjectorScreen', () => {
  it('rotates all waiting students while keeping the current call visible', () => {
    render(<ProjectorScreen sessionId={22} />);
    const waiting = screen.getByRole('list', { name: 'Ожидающие студенты' });
    expect(within(waiting).getAllByRole('listitem')).toHaveLength(4);
    expect(within(waiting).getByText('Ожидающий 2')).toBeInTheDocument();
    expect(within(waiting).queryByText('Ожидающий 6')).not.toBeInTheDocument();
    act(() => { vi.advanceTimersByTime(10_000); });
    expect(screen.getByText('Страница 2 из 2')).toBeInTheDocument();
    expect(within(waiting).getByText('Ожидающий 6')).toBeInTheDocument();
    expect(within(waiting).getByText('Ожидающий 8')).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Вызванный студент' })).toBeInTheDocument();
    act(() => { vi.advanceTimersByTime(10_000); });
    expect(within(waiting).getByText('Ожидающий 2')).toBeInTheDocument();
  });

  it('clamps pagination immediately when a live queue shrinks and preserves a call after closing registration', () => {
    const view = render(<ProjectorScreen sessionId={22} />);
    fireEvent.click(screen.getByRole('button', { name: 'Следующая страница очереди' }));
    const updated = snapshot(1);
    updated.session.registration_open = false;
    vi.mocked(useQueueDisplay).mockReturnValue(state({ snapshot: updated }));
    view.rerender(<ProjectorScreen sessionId={22} />);
    expect(within(screen.getByRole('list', { name: 'Ожидающие студенты' })).getByText('Ожидающий 2')).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Вызванный студент' })).toBeInTheDocument();
    expect(screen.getByText('Запись закрыта · очередь продолжает обслуживаться')).toBeInTheDocument();
    expect(screen.queryByText(/Страница/)).not.toBeInTheDocument();
  });

  it('marks the last snapshot as outdated even when an update is still waiting for a response', () => {
    render(<ProjectorScreen sessionId={22} />);
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
    act(() => { vi.advanceTimersByTime(11_000); });
    expect(screen.getByRole('alert')).toHaveTextContent('Нет актуального обновления');
    expect(screen.getByText('Данные устарели')).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Вызванный студент' })).toBeInTheDocument();
  });

  it('hides personal data on access loss and shows a sign-in link', () => {
    const view = render(<ProjectorScreen sessionId={22} />);
    vi.mocked(useQueueDisplay).mockReturnValue(state({ access: 'login-required' }));
    view.rerender(<ProjectorScreen sessionId={22} />);
    expect(screen.queryByText('Вызванный студент')).not.toBeInTheDocument();
    expect(screen.queryByText('Ожидающий 2')).not.toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Войдите как преподаватель' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Открыть основную страницу' })).toHaveAttribute('href', 'http://localhost:8080/');
  });

  it('handles a rejected fullscreen request without losing the queue', async () => {
    const requestFullscreen = vi.fn().mockRejectedValue(new Error('not allowed'));
    Object.defineProperty(document.documentElement, 'requestFullscreen', { configurable: true, value: requestFullscreen });
    render(<ProjectorScreen sessionId={22} />);
    await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'Во весь экран' })); });
    expect(requestFullscreen).toHaveBeenCalledOnce();
    expect(screen.getByText('Браузер не разрешил полный экран. Разверните окно средствами браузера.')).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Вызванный студент' })).toBeInTheDocument();
  });

  it('fits pages to the measured tallest name instead of hiding lower students below the screen', () => {
    vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockImplementation(function (this: HTMLElement) {
      const height = this.classList.contains('projector-entry') ? 180 : 22;
      const top = this.tagName === 'OL' ? 300 : 0;
      return { x: 0, y: top, top, bottom: top + height, left: 0, right: 600, width: 600, height, toJSON: () => ({}) } as DOMRect;
    });
    render(<ProjectorScreen sessionId={22} />);
    const waiting = screen.getByRole('list', { name: 'Ожидающие студенты' });
    expect(within(waiting).getAllByRole('listitem')).toHaveLength(1);
    expect(screen.getByText('Страница 1 из 7')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Следующая страница очереди' }));
    expect(within(waiting).getByText('Ожидающий 3')).toBeInTheDocument();
  });
});
