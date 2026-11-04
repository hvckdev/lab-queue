import { StrictMode, type PropsWithChildren } from 'react';
import { act, renderHook, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { Entry, QueueSnapshot, Session, User } from '../types';
import { useLabQueue } from './useLabQueue';
import { LabQueueStore } from './controller';

const teacher: User = { id: 1, name: 'Преподаватель', email: 'teacher@example.com', role: 'teacher', group_id: null };
const secondTeacher: User = { ...teacher, id: 2, name: 'Другой преподаватель', email: 'other@example.com' };
const student: User = { id: 3, name: 'Студент', email: 'student@example.com', role: 'student', group_id: 1 };
const session = (id: number): Session => ({ id, teacher_id: 1, subject_id: 1, subject_name: 'Программирование', group_id: 1, group_name: 'ИВТ-101', lab_title: `Лабораторная ${id}`, starts_at: '2026-10-28T10:00:00Z', ends_at: '2028-10-28T12:00:00Z', registration_closed: false, finished: false, registration_open: true, active_count: 0 });
const entry: Entry = { id: 42, session_id: 1, student_id: 3, student_name: 'Студент', ticket_number: 7, position: 1, status: 'waiting', created_at: '2026-10-28T11:00:00Z' };
function json(status: number, body: unknown): Response { return { status, ok: status >= 200 && status < 300, json: async () => body } as Response; }
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
}

function server(identity: User | null = teacher) {
  let current = identity;
  let mine: Entry | null = null;
  const overrides = new Map<string, (init: RequestInit) => Response | Promise<Response>>();
  const snapshot = (id: number): QueueSnapshot => ({ session: session(id), entries: id === 1 && mine ? [mine] : [], my_entry: id === 1 ? mine : null, next_position: mine ? 2 : 1 });
  const fetch = vi.fn(async (input: RequestInfo | URL, options: RequestInit = {}) => {
    const path = String(input);
    const override = overrides.get(path);
    if (override) return override(options);
    if (path === '/api/groups') return json(200, { groups: [{ id: 1, name: 'ИВТ-101' }] });
    if (path === '/api/auth/me') return current ? json(200, { user: current }) : json(401, { error: { message: 'Войдите' } });
    if (path === '/api/auth/login' || path === '/api/auth/register') {
      const body = JSON.parse(String(options.body)) as { email: string };
      current = body.email === secondTeacher.email ? secondTeacher : body.email === student.email ? student : teacher;
      return json(path.endsWith('register') ? 201 : 200, { user: current });
    }
    if (path === '/api/auth/logout') { current = null; return json(200, { ok: true }); }
    if (path === '/api/subjects') return json(200, { subjects: [{ id: 1, name: 'Программирование' }] });
    if (path.startsWith('/api/sessions?')) return json(200, { sessions: [session(1), session(2)] });
    const queue = path.match(/^\/api\/sessions\/(\d+)\/queue$/);
    if (queue) return json(200, snapshot(Number(queue[1])));
    throw new Error(`Unexpected request ${path}`);
  });
  vi.stubGlobal('fetch', fetch);
  return { overrides, fetch, setMine: (value: Entry | null) => { mine = value; } };
}

const strict = ({ children }: PropsWithChildren) => <StrictMode>{children}</StrictMode>;
const stores: LabQueueStore[] = [];
afterEach(() => { stores.splice(0).forEach((store) => store.stop()); vi.unstubAllGlobals(); });
function standaloneStore() { const store = new LabQueueStore(); stores.push(store); store.start(); return store; }

describe('queue controller lifetimes', () => {
  it.each([teacher, null])('refreshes groups for an existing %s view without bootstrapping it again', async (identity) => {
    const api = server(identity);
    const { result } = renderHook(useLabQueue);
    await waitFor(() => expect(result.current.booting).toBe(false));
    if (identity) await waitFor(() => expect(result.current.queue?.session.id).toBe(1));
    const identityReads = api.fetch.mock.calls.filter(([path]) => path === '/api/auth/me').length;
    const delayed = deferred<Response>();
    api.overrides.set('/api/groups', () => delayed.promise);
    let refresh!: Promise<void>;
    act(() => { refresh = result.current.refresh(); });
    expect(result.current.booting).toBe(false);
    expect(result.current.groups).toHaveLength(1);
    await act(async () => { delayed.resolve(json(200, { groups: [{ id: 1, name: 'ИВТ-101' }, { id: 3, name: 'ИВТ-103' }] })); await refresh; });
    expect(result.current.groups).toContainEqual({ id: 3, name: 'ИВТ-103' });
    expect(result.current.user?.id ?? null).toBe(identity?.id ?? null);
    expect(result.current.booting).toBe(false);
    expect(api.fetch.mock.calls.filter(([path]) => path === '/api/auth/me')).toHaveLength(identityReads);
    if (!identity) expect(api.fetch.mock.calls.some(([path]) => String(path).startsWith('/api/sessions'))).toBe(false);
  });

  it('keeps the catalogue on a failed group refresh and clears the group error after recovery', async () => {
    const api = server(null);
    const { result } = renderHook(useLabQueue);
    await waitFor(() => expect(result.current.booting).toBe(false));
    api.overrides.set('/api/groups', () => { throw new TypeError('offline'); });
    await act(async () => { await result.current.refresh(); });
    expect(result.current.bootstrapError).toContain('учебные группы');
    expect(result.current.groups).toHaveLength(1);
    api.overrides.set('/api/groups', () => json(200, { groups: [{ id: 3, name: 'ИВТ-103' }] }));
    await act(async () => { await result.current.refresh(); });
    expect(result.current.bootstrapError).toBeNull();
    expect(result.current.groups).toEqual([{ id: 3, name: 'ИВТ-103' }]);
  });

  it('ignores group replies from an earlier login while another account is active', async () => {
    const api = server();
    const { result } = renderHook(useLabQueue);
    await waitFor(() => expect(result.current.queue?.session.id).toBe(1));
    const oldGroups = deferred<Response>();
    api.overrides.set('/api/groups', () => oldGroups.promise);
    let oldRead!: Promise<void>;
    act(() => { oldRead = result.current.refresh(); });
    await act(async () => { await result.current.logout(); });
    api.overrides.set('/api/groups', () => json(200, { groups: [{ id: 2, name: 'Новая группа' }] }));
    await act(async () => { await result.current.login({ email: secondTeacher.email, password: 'Password123!' }); });
    expect(result.current.user?.id).toBe(secondTeacher.id);
    expect(result.current.groups).toEqual([{ id: 2, name: 'Новая группа' }]);
    await act(async () => { oldGroups.resolve(json(200, { groups: [{ id: 99, name: 'Старый ответ' }] })); await oldRead; });
    expect(result.current.groups).toEqual([{ id: 2, name: 'Новая группа' }]);
    expect(result.current.user?.id).toBe(secondTeacher.id);
  });

  it('restores an administrator without requesting queues or allowing queue mutations', async () => {
    const admin: User = { ...teacher, id: 90, role: 'admin' };
    const api = server(admin);
    const { result } = renderHook(useLabQueue);
    await waitFor(() => expect(result.current.booting).toBe(false));
    expect(result.current.user?.role).toBe('admin');
    expect(result.current.syncError).toBeNull();
    expect(api.fetch.mock.calls.some(([path]) => String(path).startsWith('/api/sessions'))).toBe(false);
    await act(async () => {
      expect(await result.current.createSession({ subject_id: 1, group_id: 1, lab_title: 'Недоступная команда', starts_at: '2030-01-20T10:00:00Z', ends_at: '2030-01-20T12:00:00Z' })).toBeNull();
    });
    expect(api.fetch.mock.calls.some(([path]) => path === '/api/sessions')).toBe(false);
  });

  it('refreshes the subject catalogue for an already signed-in teacher', async () => {
    const api = server();
    const { result } = renderHook(useLabQueue);
    await waitFor(() => expect(result.current.queue?.session.id).toBe(1));
    api.overrides.set('/api/subjects', () => json(200, { subjects: [{ id: 1, name: 'Программирование' }, { id: 4, name: 'Математика' }] }));
    await act(async () => { await result.current.refresh(); });
    expect(result.current.subjects).toContainEqual({ id: 4, name: 'Математика' });
  });

  it('bootstraps under StrictMode without treating an anonymous 401 as an error', async () => {
    server(null);
    const { result } = renderHook(useLabQueue, { wrapper: strict });
    await waitFor(() => expect(result.current.booting).toBe(false));
    expect(result.current.user).toBeNull();
    expect(result.current.bootstrapError).toBeNull();
    expect(result.current.notice).toBeNull();
    expect(result.current.groups).toHaveLength(1);
    await act(async () => { expect(await result.current.login({ email: teacher.email, password: 'Password123!' })).toBe(true); });
    expect(result.current.queue?.session.id).toBe(1);
  });

  it('an earlier account’s late 401 cannot sign out a new login', async () => {
    const api = server();
    const late = deferred<Response>();
    let queues = 0;
    api.overrides.set('/api/sessions/1/queue', () => ++queues === 1 ? late.promise : json(200, { session: session(1), entries: [], my_entry: null, next_position: 1 }));
    const { result } = renderHook(useLabQueue);
    await waitFor(() => expect(queues).toBe(1));
    await act(async () => { await result.current.logout(); });
    await act(async () => { await result.current.login({ email: secondTeacher.email, password: 'Password123!' }); });
    expect(result.current.user?.id).toBe(2);
    await act(async () => { late.resolve(json(401, { error: { message: 'Expired old token' } })); });
    expect(result.current.user?.id).toBe(2);
  });

  it('a selection change does not hide expiry of the current login', async () => {
    const api = server();
    const late = deferred<Response>();
    api.overrides.set('/api/sessions/1/queue', () => late.promise);
    const { result } = renderHook(useLabQueue);
    await waitFor(() => expect(result.current.sessions).toHaveLength(2));
    act(() => { result.current.selectSession(2); });
    await waitFor(() => expect(result.current.queue?.session.id).toBe(2));
    await act(async () => { late.resolve(json(401, { error: { message: 'Current token expired' } })); });
    expect(result.current.user).toBeNull();
    expect(result.current.notice?.message).toContain('Сеанс истёк');
  });

  it('ignores a late successful queue response after selecting another session', async () => {
    const api = server();
    const late = deferred<Response>();
    api.overrides.set('/api/sessions/1/queue', () => late.promise);
    const { result } = renderHook(useLabQueue);
    await waitFor(() => expect(result.current.sessions).toHaveLength(2));
    act(() => { result.current.selectSession(2); });
    await waitFor(() => expect(result.current.queue?.session.id).toBe(2));
    await act(async () => { late.resolve(json(200, { session: session(1), entries: [entry], my_entry: entry, next_position: 2 })); });
    expect(result.current.selectedId).toBe(2);
    expect(result.current.queue?.session.id).toBe(2);
    expect(result.current.queue?.entries).toHaveLength(0);
  });

  it('reuses an uncertain join’s idempotency key when the student retries', async () => {
    const api = server(student);
    const keys: string[] = [];
    api.overrides.set('/api/sessions/1/entries', (options) => {
      keys.push(new Headers(options.headers).get('Idempotency-Key') ?? '');
      if (keys.length === 1) throw new TypeError('connection lost');
      api.setMine(entry);
      return json(201, { entry });
    });
    const { result } = renderHook(useLabQueue);
    await waitFor(() => expect(result.current.queue?.session.id).toBe(1));
    await act(async () => { expect(await result.current.joinQueue()).toBe(false); });
    expect(result.current.pending.join).toBe(true);
    await act(async () => { expect(await result.current.joinQueue()).toBe(true); });
    expect(keys).toHaveLength(2);
    expect(keys[0]).not.toBe('');
    expect(keys[1]).toBe(keys[0]);
    expect(result.current.queue?.my_entry?.ticket_number).toBe(7);
    expect(result.current.pending.join).toBe(false);
  });

  it('blocks a second click before React renders the busy state', async () => {
    const api = server(student);
    const reply = deferred<Response>();
    api.overrides.set('/api/sessions/1/entries', () => reply.promise);
    const { result } = renderHook(useLabQueue);
    await waitFor(() => expect(result.current.queue?.session.id).toBe(1));
    let first!: Promise<boolean>;
    let second!: Promise<boolean>;
    act(() => { first = result.current.joinQueue(); second = result.current.joinQueue(); });
    expect(await second).toBe(false);
    expect(api.fetch.mock.calls.filter(([path]) => path === '/api/sessions/1/entries')).toHaveLength(1);
    api.setMine(entry);
    await act(async () => { reply.resolve(json(201, { entry })); expect(await first).toBe(true); });
    expect(result.current.busy).toBe(false);
  });
});

describe('preserved store effect restarts', () => {
  const input = { subject_id: 1, group_id: 1, lab_title: 'Лабораторная', starts_at: '2026-10-28T10:00:00Z', ends_at: '2028-10-28T12:00:00Z' };

  it('unblocks an interrupted mutation and preserves its key without letting its late result settle the new operation', async () => {
    const api = server();
    const oldReply = deferred<Response>();
    const newReply = deferred<Response>();
    const keys: string[] = [];
    api.overrides.set('/api/sessions', (options) => {
      keys.push(new Headers(options.headers).get('Idempotency-Key') ?? '');
      return keys.length === 1 ? oldReply.promise : newReply.promise;
    });
    const store = standaloneStore();
    await waitFor(() => expect(store.getSnapshot().queue?.session.id).toBe(1));
    const oldOperation = store.createSession(input);
    expect(store.getSnapshot().busy).toBe(true);
    store.stop();
    store.start();
    expect(store.getSnapshot()).toMatchObject({ busy: false, authBusy: false, loading: false });
    await waitFor(() => expect(store.getSnapshot().booting).toBe(false));
    await waitFor(() => expect(store.getSnapshot().queue?.session.id).toBe(1));
    expect(store.getSnapshot().pending.create).toBe(true);
    const newOperation = store.createSession(input);
    expect(store.getSnapshot().busy).toBe(true);
    expect(keys).toHaveLength(2);
    expect(keys[1]).toBe(keys[0]);
    oldReply.resolve(json(201, { session: session(91) }));
    expect(await oldOperation).toBeNull();
    expect(store.getSnapshot().busy).toBe(true);
    expect(store.getSnapshot().pending.create).toBe(true);
    expect(store.getSnapshot().selectedId).toBe(1);
    newReply.resolve(json(201, { session: session(91) }));
    expect((await newOperation)?.id).toBe(91);
    expect(store.getSnapshot().busy).toBe(false);
    expect(store.getSnapshot().pending.create).toBe(false);
  });

  it('retains the key of a committed create with a lost response after restoring the same teacher', async () => {
    const api = server();
    const keys: string[] = [];
    api.overrides.set('/api/sessions', (options) => {
      keys.push(new Headers(options.headers).get('Idempotency-Key') ?? '');
      if (keys.length === 1) throw new TypeError('response lost after commit');
      return json(201, { session: session(91) });
    });
    const store = standaloneStore();
    await waitFor(() => expect(store.getSnapshot().queue?.session.id).toBe(1));
    expect(await store.createSession(input)).toBeNull();
    expect(store.getSnapshot().pending.create).toBe(true);
    store.stop();
    store.start();
    await waitFor(() => expect(store.getSnapshot().booting).toBe(false));
    await waitFor(() => expect(store.getSnapshot().queue?.session.id).toBe(1));
    expect(store.getSnapshot().pending.create).toBe(true);
    expect((await store.createSession(input))?.id).toBe(91);
    expect(keys).toHaveLength(2);
    expect(keys[1]).toBe(keys[0]);
  });

  it('unblocks an interrupted login and ignores its late result while another login is pending', async () => {
    const api = server(null);
    const oldReply = deferred<Response>();
    const newReply = deferred<Response>();
    let logins = 0;
    api.overrides.set('/api/auth/login', () => ++logins === 1 ? oldReply.promise : newReply.promise);
    const store = standaloneStore();
    await waitFor(() => expect(store.getSnapshot().booting).toBe(false));
    const oldLogin = store.login({ email: teacher.email, password: 'Password123!' });
    expect(store.getSnapshot().authBusy).toBe(true);
    store.stop();
    store.start();
    expect(store.getSnapshot()).toMatchObject({ busy: false, authBusy: false, loading: false });
    await waitFor(() => expect(store.getSnapshot().booting).toBe(false));
    const newLogin = store.login({ email: secondTeacher.email, password: 'Password123!' });
    expect(logins).toBe(2);
    oldReply.resolve(json(200, { user: teacher }));
    expect(await oldLogin).toBe(false);
    expect(store.getSnapshot().user).toBeNull();
    expect(store.getSnapshot().authBusy).toBe(true);
    newReply.resolve(json(200, { user: secondTeacher }));
    expect(await newLogin).toBe(true);
    expect(store.getSnapshot().user?.id).toBe(2);
    expect(store.getSnapshot().authBusy).toBe(false);
  });
});
