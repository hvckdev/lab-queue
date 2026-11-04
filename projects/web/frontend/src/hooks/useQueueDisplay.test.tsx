import { StrictMode, type PropsWithChildren } from 'react';
import { act, renderHook } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { Entry, QueueSnapshot, Session, User } from '../types';
import { useQueueDisplay } from './useQueueDisplay';

const teacher: User = { id: 1, name: 'Преподаватель', email: 'teacher@example.com', role: 'teacher', group_id: null };
const student: User = { ...teacher, id: 3, role: 'student', group_id: 1 };
const session = (id: number): Session => ({ id, teacher_id: 1, subject_id: 1, subject_name: 'Программирование', group_id: 1,
  group_name: 'ИВТ-101', lab_title: `Лабораторная ${id}`, starts_at: '2026-11-04T10:00:00Z', ends_at: '2026-11-04T12:00:00Z',
  registration_closed: false, finished: false, registration_open: true, active_count: 0 });
const entry: Entry = { id: 8, session_id: 1, student_id: 3, student_name: 'Студент', ticket_number: 5,
  position: 1, status: 'waiting', created_at: '2026-11-04T10:00:00Z' };
const snapshot = (id = 1, entries: Entry[] = []): QueueSnapshot => ({ session: session(id), entries, my_entry: null, next_position: entries.length + 1 });
const json = (status: number, body: unknown): Response => ({ status, ok: status >= 200 && status < 300, json: async () => body } as Response);
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
}

function server() {
  let identity: User | null = teacher;
  let entries: Entry[] = [entry];
  const overrides = new Map<string, (options: RequestInit) => Response | Promise<Response>>();
  const fetch = vi.fn(async (input: RequestInfo | URL, options: RequestInit = {}) => {
    const path = String(input);
    const override = overrides.get(path);
    if (override) return override(options);
    if (path === '/api/auth/me') return identity ? json(200, { user: identity }) : json(401, {});
    const match = path.match(/^\/api\/sessions\/(\d+)\/queue$/);
    if (match) return json(200, snapshot(Number(match[1]), entries));
    throw new Error(`Unexpected request ${path}`);
  });
  vi.stubGlobal('fetch', fetch);
  return { fetch, overrides, setIdentity: (value: User | null) => { identity = value; }, setEntries: (value: Entry[]) => { entries = value; } };
}

const tick = async (milliseconds = 0) => { await act(async () => { await vi.advanceTimersByTimeAsync(milliseconds); }); };
beforeEach(() => { vi.useFakeTimers(); });
afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals(); });

describe('projector queue reads', () => {
  it('polls the pinned session every three seconds and reflects called and waiting entries', async () => {
    const api = server();
    const { result } = renderHook(() => useQueueDisplay(1));
    await tick();
    expect(result.current.access).toBe('allowed');
    expect(result.current.snapshot?.entries[0].status).toBe('waiting');
    const firstUpdated = result.current.lastUpdated;
    api.setEntries([{ ...entry, status: 'called' }, { ...entry, id: 9, student_id: 4, position: 2, ticket_number: 6 }]);
    await tick(2999);
    expect(result.current.snapshot?.entries).toHaveLength(1);
    await tick(1);
    expect(result.current.snapshot?.entries).toHaveLength(2);
    expect(result.current.snapshot?.entries[0].status).toBe('called');
    expect(result.current.lastUpdated!.getTime()).toBeGreaterThan(firstUpdated!.getTime());
    expect(api.fetch.mock.calls.every(([path, options]) => /^\/api\/(auth\/me|sessions\/1\/queue)$/.test(String(path)) && options?.method === 'GET')).toBe(true);
  });

  it('discards late responses and immediately hides data when the session changes', async () => {
    const api = server();
    const late = deferred<Response>();
    const { result, rerender } = renderHook(({ id }) => useQueueDisplay(id), { initialProps: { id: 1 } });
    await tick();
    api.overrides.set('/api/sessions/1/queue', () => late.promise);
    act(() => result.current.refresh());
    await tick();
    rerender({ id: 2 });
    expect(result.current.snapshot).toBeNull();
    await tick();
    expect(result.current.snapshot?.session.id).toBe(2);
    await act(async () => { late.resolve(json(200, snapshot(1, [entry]))); });
    expect(result.current.snapshot?.session.id).toBe(2);
  });

  it.each(['login', 'forbidden', 'student'] as const)('clears personal data after access changes: %s', async (mode) => {
    const api = server();
    const { result } = renderHook(() => useQueueDisplay(1));
    await tick();
    expect(result.current.snapshot).not.toBeNull();
    if (mode === 'login') api.setIdentity(null);
    else if (mode === 'student') api.setIdentity(student);
    else api.overrides.set('/api/sessions/1/queue', () => json(403, {}));
    await tick(3000);
    expect(result.current.snapshot).toBeNull();
    expect(result.current.lastUpdated).toBeNull();
    expect(result.current.access).toBe(mode === 'login' ? 'login-required' : 'forbidden');
    api.setIdentity(teacher);
    api.overrides.clear();
    await tick(3000);
    expect(result.current.access).toBe('allowed');
    expect(result.current.snapshot?.entries).toHaveLength(1);
  });

  it('hides the former teacher’s data before fetching the new teacher’s queue', async () => {
    const api = server();
    const { result } = renderHook(() => useQueueDisplay(1));
    await tick();
    const next = deferred<Response>();
    api.setIdentity({ ...teacher, id: 2 });
    api.overrides.set('/api/sessions/1/queue', () => next.promise);
    await tick(3000);
    expect(result.current.snapshot).toBeNull();
    expect(result.current.lastUpdated).toBeNull();
    await act(async () => { next.resolve(json(200, snapshot(1, [entry]))); });
    expect(result.current.access).toBe('forbidden');
    expect(result.current.snapshot).toBeNull();
  });

  it('marks retained data as stale during a network failure and recovers on reconnect', async () => {
    const api = server();
    const { result } = renderHook(() => useQueueDisplay(1));
    await tick();
    const previous = result.current.snapshot;
    const previousUpdated = result.current.lastUpdated;
    api.overrides.set('/api/auth/me', () => { throw new TypeError('offline'); });
    await tick(3000);
    expect(result.current.snapshot).toBe(previous);
    expect(result.current.lastUpdated).toBe(previousUpdated);
    expect(result.current.error).toContain('Не удалось обновить очередь');
    expect(result.current.error).not.toContain('Действие');
    api.overrides.clear();
    api.setEntries([]);
    act(() => window.dispatchEvent(new Event('online')));
    await tick();
    expect(result.current.error).toBeNull();
    expect(result.current.snapshot?.entries).toHaveLength(0);
  });

  it('serializes slow reads and aborts the current StrictMode lifetime on unmount', async () => {
    const api = server();
    const late = deferred<Response>();
    let queueSignal: AbortSignal | null = null;
    api.overrides.set('/api/sessions/1/queue', (options) => { queueSignal = options.signal as AbortSignal; return late.promise; });
    const strict = ({ children }: PropsWithChildren) => <StrictMode>{children}</StrictMode>;
    const { unmount } = renderHook(() => useQueueDisplay(1), { wrapper: strict });
    await tick();
    await tick(9000);
    expect(api.fetch.mock.calls.filter(([path]) => path === '/api/sessions/1/queue')).toHaveLength(1);
    const requests = api.fetch.mock.calls.length;
    unmount();
    expect(queueSignal!.aborted).toBe(true);
    await act(async () => { late.resolve(json(200, snapshot(1, [entry]))); });
    await tick(6000);
    expect(api.fetch.mock.calls).toHaveLength(requests);
    expect(vi.getTimerCount()).toBe(0);
  });
});
