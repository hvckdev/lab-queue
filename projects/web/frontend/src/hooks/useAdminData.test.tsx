import { StrictMode, type PropsWithChildren } from 'react';
import { act, renderHook, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { User } from '../types';
import { useAdminData } from './useAdminData';

const admin: User = { id: 1, name: 'Администратор', email: 'admin@example.com', role: 'admin', group_id: null };
const json = (status: number, body: unknown) => ({ status, ok: status >= 200 && status < 300, json: async () => body }) as Response;
function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>((done) => { resolve = done; }); return { promise, resolve }; }
function server() {
  let users: User[] = [admin];
  let subjects = [{ id: 1, name: 'Программирование' }];
  let groups = [{ id: 1, name: 'ИВТ-101' }];
  const overrides = new Map<string, (options: RequestInit) => Response | Promise<Response>>();
  const fetch = vi.fn(async (input: RequestInfo | URL, options: RequestInit = {}) => {
    const path = String(input);
    const override = overrides.get(`${options.method || 'GET'} ${path}`);
    if (override) return override(options);
    if (path.startsWith('/api/admin/users?')) return json(200, { users, total: users.length });
    if (path === '/api/subjects') return json(200, { subjects });
    if (path === '/api/groups') return json(200, { groups });
    if (path === '/api/admin/users') { const body = JSON.parse(String(options.body)); const user = { ...body, id: users.length + 1 }; delete user.password; users = [...users, user]; return json(201, { user }); }
    if (path === '/api/admin/subjects') { const body = JSON.parse(String(options.body)); const subject = { id: subjects.length + 1, name: body.name }; subjects = [...subjects, subject]; return json(201, { subject }); }
    if (path === '/api/admin/groups') { const body = JSON.parse(String(options.body)); const group = { id: groups.length + 1, name: body.name }; groups = [...groups, group]; return json(201, { group }); }
    throw new Error(`Unexpected request ${path}`);
  });
  vi.stubGlobal('fetch', fetch);
  return { fetch, overrides };
}
afterEach(() => vi.unstubAllGlobals());

describe('admin data scope and operations', () => {
  it('adds a group immediately and blocks a second mutation while the first is pending', async () => {
    const api = server();
    const pending = deferred<Response>();
    const reread = deferred<Response>();
    api.overrides.set('POST /api/admin/groups', () => pending.promise);
    const { result } = renderHook(() => useAdminData(admin, vi.fn()));
    await waitFor(() => expect(result.current.groups).toHaveLength(1));
    let operation!: Promise<boolean>;
    act(() => { operation = result.current.createGroup('ИВТ-103'); });
    expect(await result.current.createGroup('ИВТ-103')).toBe(false);
    expect(api.fetch.mock.calls.filter(([path]) => path === '/api/admin/groups')).toHaveLength(1);
    api.overrides.set('GET /api/groups', () => reread.promise);
    await act(async () => { pending.resolve(json(201, { group: { id: 3, name: 'ИВТ-103' } })); expect(await operation).toBe(true); });
    expect(result.current.groups).toContainEqual({ id: 3, name: 'ИВТ-103' });
    expect(result.current.notice).toBe('Группа добавлена.');
    await act(async () => { reread.resolve(json(200, { groups: [{ id: 1, name: 'ИВТ-101' }, { id: 3, name: 'ИВТ-103' }] })); });
    await waitFor(() => expect(result.current.loading).toBe(false));
  });

  it('loads the next page explicitly and refreshes after creation without authenticating as the new user', async () => {
    const api = server();
    const last: User = { ...admin, id: 51, name: 'Последний пользователь' };
    api.overrides.set('GET /api/admin/users?limit=50&offset=0', () => json(200, { users: [admin], total: 51 }));
    api.overrides.set('GET /api/admin/users?limit=50&offset=50', () => json(200, { users: [last], total: 51 }));
    const { result } = renderHook(() => useAdminData(admin, vi.fn()));
    await waitFor(() => expect(result.current.loading).toBe(false));
    act(() => { result.current.setPageOffset(50); });
    await waitFor(() => expect(result.current.users[0]?.id).toBe(51));
    expect(result.current.offset).toBe(50);
    await act(async () => { expect(await result.current.createUser({ name: 'Учитель', email: 'teacher-new@example.com', password: 'ExamplePass123!', role: 'teacher', group_id: null })).toBe(true); });
    await waitFor(() => expect(result.current.offset).toBe(0));
    expect(api.fetch.mock.calls.some(([path]) => String(path).startsWith('/api/auth/'))).toBe(false);
    const post = api.fetch.mock.calls.find(([path]) => path === '/api/admin/users');
    expect(JSON.parse(String(post?.[1]?.body))).toMatchObject({ role: 'teacher', group_id: null });
  });

  it('blocks overlapping mutations and refreshes the subjects after a successful create', async () => {
    const api = server();
    const pending = deferred<Response>();
    api.overrides.set('POST /api/admin/subjects', () => pending.promise);
    const { result } = renderHook(() => useAdminData(admin, vi.fn()));
    await waitFor(() => expect(result.current.loading).toBe(false));
    let first!: Promise<boolean>;
    act(() => { first = result.current.createSubject('Математика'); });
    expect(await result.current.createSubject('Математика')).toBe(false);
    expect(api.fetch.mock.calls.filter(([path]) => path === '/api/admin/subjects')).toHaveLength(1);
    api.overrides.set('GET /api/subjects', () => json(200, { subjects: [{ id: 1, name: 'Программирование' }, { id: 2, name: 'Математика' }] }));
    await act(async () => { pending.resolve(json(201, { subject: { id: 2, name: 'Математика' } })); expect(await first).toBe(true); });
    await waitFor(() => expect(result.current.subjects).toHaveLength(2));
    expect(result.current.notice).toBe('Предмет добавлен.');
  });

  it('does not automatically repeat an uncertain POST', async () => {
    const api = server();
    api.overrides.set('POST /api/admin/subjects', () => { throw new TypeError('response lost'); });
    const { result } = renderHook(() => useAdminData(admin, vi.fn()));
    await waitFor(() => expect(result.current.loading).toBe(false));
    await act(async () => { expect(await result.current.createSubject('Математика')).toBe(false); });
    expect(result.current.error).toContain('Обновите список');
    act(() => { result.current.refresh(); });
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(api.fetch.mock.calls.filter(([path]) => path === '/api/admin/subjects')).toHaveLength(1);
  });

  it('clears private lists and invalidates the current identity on a protected 401', async () => {
    const api = server();
    const expired = vi.fn();
    const { result } = renderHook(() => useAdminData(admin, expired));
    await waitFor(() => expect(result.current.users).toHaveLength(1));
    api.overrides.set('GET /api/admin/users?limit=50&offset=0', () => json(401, { error: { message: 'Expired' } }));
    act(() => { result.current.refresh(); });
    await waitFor(() => expect(expired).toHaveBeenCalledWith(admin.id));
    expect(result.current.users).toHaveLength(0);
    expect(result.current.subjects).toHaveLength(0);
  });

  it('ignores late results belonging to another administrator', async () => {
    const api = server();
    const old = deferred<Response>();
    api.overrides.set('GET /api/admin/users?limit=50&offset=0', () => old.promise);
    const expired = vi.fn();
    const { result, rerender } = renderHook(({ user }) => useAdminData(user, expired), { initialProps: { user: admin } });
    await waitFor(() => expect(api.fetch.mock.calls.some(([path]) => String(path).startsWith('/api/admin/users?'))).toBe(true));
    const other = { ...admin, id: 2, name: 'Второй администратор' };
    api.overrides.set('GET /api/admin/users?limit=50&offset=0', () => json(200, { users: [other], total: 1 }));
    rerender({ user: other });
    await waitFor(() => expect(result.current.users[0]?.id).toBe(2));
    await act(async () => { old.resolve(json(401, { error: { message: 'Old identity expired' } })); });
    expect(expired).not.toHaveBeenCalled();
    expect(result.current.users[0]?.id).toBe(2);
  });

  it('survives StrictMode effect restarts and stops callbacks after unmount', async () => {
    const api = server();
    const expired = vi.fn();
    const wrapper = ({ children }: PropsWithChildren) => <StrictMode>{children}</StrictMode>;
    const { result, unmount } = renderHook(() => useAdminData(admin, expired), { wrapper });
    await waitFor(() => expect(result.current.users).toHaveLength(1));
    const late = deferred<Response>();
    api.overrides.set('POST /api/admin/subjects', () => late.promise);
    let operation!: Promise<boolean>;
    act(() => { operation = result.current.createSubject('Математика'); });
    unmount();
    await act(async () => { late.resolve(json(401, { error: { message: 'Expired' } })); expect(await operation).toBe(false); });
    expect(expired).not.toHaveBeenCalled();
  });
});
