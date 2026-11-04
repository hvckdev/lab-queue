import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { LabQueueController, User } from '../types';
import { AdminDashboard } from './AdminDashboard';

const admin: User = { id: 1, name: 'Администратор', email: 'admin@example.com', role: 'admin', group_id: null };
const json = (status: number, body: unknown) => ({ status, ok: status >= 200 && status < 300, json: async () => body }) as Response;
const app = () => ({ user: admin, groups: [{ id: 1, name: 'ИВТ-101' }], logout: vi.fn(async () => true), endExpiredSession: vi.fn(), authBusy: false, syncError: null } as unknown as LabQueueController);
afterEach(() => vi.unstubAllGlobals());

describe('admin forms', () => {
  it('adds a trimmed group and makes it selectable for a student without reloading', async () => {
    let groups = [{ id: 1, name: 'ИВТ-101' }];
    const fetch = vi.fn(async (path: RequestInfo | URL, options: RequestInit = {}) => {
      if (String(path).startsWith('/api/admin/users?')) return json(200, { users: [admin], total: 1 });
      if (path === '/api/subjects') return json(200, { subjects: [] });
      if (path === '/api/groups') return json(200, { groups });
      if (path === '/api/admin/groups') { const { name } = JSON.parse(String(options.body)); const group = { id: 3, name }; groups = [...groups, group]; return json(201, { group }); }
      throw new Error(`Unexpected request ${path}`);
    });
    vi.stubGlobal('fetch', fetch);
    render(<AdminDashboard app={app()} />);
    await waitFor(() => expect(screen.getByRole('list', { name: 'Группы' })).toHaveTextContent('ИВТ-101'));
    fireEvent.change(screen.getByLabelText('Название группы'), { target: { value: '  ИВТ-103  ' } });
    fireEvent.click(screen.getByRole('button', { name: 'Добавить группу' }));
    await waitFor(() => expect(screen.getByRole('list', { name: 'Группы' })).toHaveTextContent('ИВТ-103'));
    expect(screen.getByLabelText('Название группы')).toHaveValue('');
    expect(screen.getByLabelText('Учебная группа')).toHaveTextContent('ИВТ-103');
    fireEvent.change(screen.getByLabelText('Учебная группа'), { target: { value: '3' } });
    expect(screen.getByLabelText('Учебная группа')).toHaveValue('3');
    const post = fetch.mock.calls.find(([path]) => path === '/api/admin/groups');
    expect(JSON.parse(String(post?.[1]?.body))).toEqual({ name: 'ИВТ-103' });
  });

  it.each(['duplicate', 'network'])('keeps the group name after a %s error', async (failure) => {
    let reloadFails = false;
    const fetch = vi.fn(async (path: RequestInfo | URL) => {
      if (String(path).startsWith('/api/admin/users?')) return reloadFails ? json(503, { error: { message: 'Unavailable' } }) : json(200, { users: [admin], total: 1 });
      if (path === '/api/subjects') return json(200, { subjects: [] });
      if (path === '/api/groups') return json(200, { groups: [{ id: 1, name: 'ИВТ-101' }] });
      if (path === '/api/admin/groups') {
        if (failure === 'network') throw new TypeError('response lost');
        return json(409, { error: { code: 'group_exists', message: 'Группа уже существует' } });
      }
      throw new Error(`Unexpected request ${path}`);
    });
    vi.stubGlobal('fetch', fetch);
    render(<AdminDashboard app={app()} />);
    await waitFor(() => expect(screen.getByRole('list', { name: 'Группы' })).toHaveTextContent('ИВТ-101'));
    fireEvent.change(screen.getByLabelText('Название группы'), { target: { value: 'ИВТ-101' } });
    fireEvent.click(screen.getByRole('button', { name: 'Добавить группу' }));
    const groupRegion = screen.getByRole('region', { name: 'Добавить группу' });
    const inlineError = await within(groupRegion).findByRole('alert');
    expect(inlineError).toHaveTextContent(failure === 'network' ? 'Обновите список' : 'Группа уже существует');
    expect(screen.getAllByRole('alert')).toHaveLength(1);
    expect(screen.getByLabelText('Название группы')).toHaveAttribute('aria-invalid', 'true');
    expect(screen.getByLabelText('Название группы')).toHaveValue('ИВТ-101');
    expect(fetch.mock.calls.filter(([path]) => path === '/api/admin/groups')).toHaveLength(1);
    reloadFails = true;
    fireEvent.click(screen.getByRole('button', { name: 'Обновить списки' }));
    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('Не удалось загрузить данные'));
    expect(within(groupRegion).queryByRole('alert')).not.toBeInTheDocument();
  });

  it('uses the selected role and clears the new user password after success', async () => {
    let users = [admin];
    const fetch = vi.fn(async (path: RequestInfo | URL, options: RequestInit = {}) => {
      if (String(path).startsWith('/api/admin/users?')) return json(200, { users, total: users.length });
      if (path === '/api/groups') return json(200, { groups: [{ id: 1, name: 'ИВТ-101' }] });
      if (path === '/api/subjects') return json(200, { subjects: [] });
      if (path === '/api/admin/users') { const input = JSON.parse(String(options.body)); const user = { ...input, id: 2 }; delete user.password; users = [...users, user]; return json(201, { user }); }
      throw new Error(`Unexpected request ${path}`);
    });
    vi.stubGlobal('fetch', fetch);
    render(<AdminDashboard app={app()} />);
    await waitFor(() => expect(screen.getByRole('list', { name: 'Пользователи' })).toHaveTextContent('admin@example.com'));
    fireEvent.change(screen.getByLabelText('Имя и фамилия'), { target: { value: 'Новый преподаватель' } });
    fireEvent.change(screen.getByLabelText('Email пользователя'), { target: { value: 'new-teacher@example.com' } });
    fireEvent.change(screen.getByLabelText('Пароль пользователя'), { target: { value: 'ExamplePass123!' } });
    fireEvent.change(screen.getByLabelText('Учебная группа'), { target: { value: '1' } });
    fireEvent.change(screen.getByLabelText('Роль'), { target: { value: 'teacher' } });
    expect(screen.queryByLabelText('Учебная группа')).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Создать пользователя' }));
    await waitFor(() => expect(screen.getByLabelText('Пароль пользователя')).toHaveValue(''));
    await waitFor(() => expect(screen.getByRole('list', { name: 'Пользователи' })).toHaveTextContent('Новый преподаватель'));
    const post = fetch.mock.calls.find(([path]) => path === '/api/admin/users');
    expect(JSON.parse(String(post?.[1]?.body))).toMatchObject({ role: 'teacher', group_id: null });
    expect(screen.getByText('Управление системой')).toBeInTheDocument();
  });

  it('adds a subject and reloads the catalogue', async () => {
    let subjects = [{ id: 1, name: 'Программирование' }];
    const fetch = vi.fn(async (path: RequestInfo | URL, options: RequestInit = {}) => {
      if (String(path).startsWith('/api/admin/users?')) return json(200, { users: [admin], total: 1 });
      if (path === '/api/groups') return json(200, { groups: [{ id: 1, name: 'ИВТ-101' }] });
      if (path === '/api/subjects') return json(200, { subjects });
      if (path === '/api/admin/subjects') { const { name } = JSON.parse(String(options.body)); const subject = { id: 2, name }; subjects = [...subjects, subject]; return json(201, { subject }); }
      throw new Error(`Unexpected request ${path}`);
    });
    vi.stubGlobal('fetch', fetch);
    render(<AdminDashboard app={app()} />);
    await screen.findByText('Программирование');
    fireEvent.change(screen.getByLabelText('Название предмета'), { target: { value: '  Математика  ' } });
    fireEvent.click(screen.getByRole('button', { name: 'Добавить предмет' }));
    expect(await screen.findByText('Математика')).toBeInTheDocument();
    expect(screen.getByLabelText('Название предмета')).toHaveValue('');
    const post = fetch.mock.calls.find(([path]) => path === '/api/admin/subjects');
    expect(JSON.parse(String(post?.[1]?.body))).toEqual({ name: 'Математика' });
  });
});
