import { StrictMode } from 'react';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { SetupGate } from './SetupGate';

const json = (status: number, body: unknown) => ({ status, ok: status >= 200 && status < 300, json: async () => body }) as Response;
const admin = { id: 1, name: 'Администратор', email: 'admin@example.com', role: 'admin', group_id: null };
function fill(password = 'ExamplePass123!') {
  fireEvent.change(screen.getByLabelText('Имя администратора'), { target: { value: 'Администратор' } });
  fireEvent.change(screen.getByLabelText('Email администратора'), { target: { value: 'admin@example.com' } });
  fireEvent.change(screen.getByLabelText('Пароль администратора'), { target: { value: password } });
  fireEvent.change(screen.getByLabelText('Повторите пароль'), { target: { value: password } });
}
const renderGate = () => render(<SetupGate><h1>Основное приложение</h1></SetupGate>);
afterEach(() => vi.unstubAllGlobals());

describe('first-run setup gate', () => {
  it('opens the application only after a confirmed completed status', async () => {
    const fetch = vi.fn(async (_path: RequestInfo | URL) => json(200, { required: false }));
    vi.stubGlobal('fetch', fetch);
    renderGate();
    expect(await screen.findByRole('heading', { name: 'Основное приложение' })).toBeInTheDocument();
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(fetch.mock.calls[0][0]).toBe('/api/setup');
  });

  it('creates an administrator once with user-supplied credentials and then removes the form', async () => {
    const fetch = vi.fn(async (_path: RequestInfo | URL, init?: RequestInit) => init?.method === 'POST' ? json(201, { user: admin }) : json(200, { required: true }));
    vi.stubGlobal('fetch', fetch);
    renderGate();
    await screen.findByRole('heading', { name: 'Создание администратора' });
    fill();
    fireEvent.click(screen.getByRole('button', { name: 'Создать администратора' }));
    expect(await screen.findByRole('heading', { name: 'Основное приложение' })).toBeInTheDocument();
    const post = fetch.mock.calls.filter(([, init]) => init?.method === 'POST');
    expect(post).toHaveLength(1);
    expect(JSON.parse(String(post[0][1]?.body))).toEqual({ name: 'Администратор', email: 'admin@example.com', password: 'ExamplePass123!' });
    expect(screen.queryByLabelText('Пароль администратора')).not.toBeInTheDocument();
  });

  it('fails closed on a status network error and allows an explicit retry', async () => {
    const fetch = vi.fn().mockRejectedValueOnce(new TypeError('offline')).mockResolvedValueOnce(json(200, { required: false }));
    vi.stubGlobal('fetch', fetch);
    renderGate();
    expect(await screen.findByRole('heading', { name: 'Система недоступна' })).toBeInTheDocument();
    expect(screen.queryByText('Основное приложение')).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Повторить' }));
    expect(await screen.findByRole('heading', { name: 'Основное приложение' })).toBeInTheDocument();
  });

  it('does not submit mismatched passwords', async () => {
    const fetch = vi.fn(async () => json(200, { required: true }));
    vi.stubGlobal('fetch', fetch);
    renderGate();
    await screen.findByRole('heading', { name: 'Создание администратора' });
    fill();
    fireEvent.change(screen.getByLabelText('Повторите пароль'), { target: { value: 'DifferentPass123!' } });
    fireEvent.click(screen.getByRole('button', { name: 'Создать администратора' }));
    expect(screen.getByRole('alert')).toHaveTextContent('Пароли не совпадают');
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it('rechecks the permanent setup flag after another request wins, without repeating the POST', async () => {
    let initialized = false;
    const fetch = vi.fn(async (_path: RequestInfo | URL, init?: RequestInit) => {
      if (init?.method === 'POST') { initialized = true; return json(409, { error: { code: 'setup_completed', message: 'Настройка завершена' } }); }
      return json(200, { required: !initialized });
    });
    vi.stubGlobal('fetch', fetch);
    renderGate();
    await screen.findByRole('heading', { name: 'Создание администратора' });
    fill();
    fireEvent.click(screen.getByRole('button', { name: 'Создать администратора' }));
    expect(await screen.findByRole('heading', { name: 'Основное приложение' })).toBeInTheDocument();
    expect(fetch.mock.calls.filter(([, init]) => init?.method === 'POST')).toHaveLength(1);
  });

  it('keeps an email conflict visible instead of treating every 409 as completed setup', async () => {
    const fetch = vi.fn(async (_path: RequestInfo | URL, init?: RequestInit) => init?.method === 'POST' ? json(409, { error: { code: 'email_exists', message: 'Email уже используется' } }) : json(200, { required: true }));
    vi.stubGlobal('fetch', fetch);
    renderGate();
    await screen.findByRole('heading', { name: 'Создание администратора' });
    fill();
    fireEvent.click(screen.getByRole('button', { name: 'Создать администратора' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Email уже используется');
    expect(screen.queryByText('Основное приложение')).not.toBeInTheDocument();
    expect(fetch).toHaveBeenCalledTimes(2);
  });

  it('ignores an earlier StrictMode status reply', async () => {
    let finish!: (response: Response) => void;
    const late = new Promise<Response>((resolve) => { finish = resolve; });
    const fetch = vi.fn().mockReturnValueOnce(late).mockResolvedValue(json(200, { required: false }));
    vi.stubGlobal('fetch', fetch);
    render(<StrictMode><SetupGate><h1>Основное приложение</h1></SetupGate></StrictMode>);
    await screen.findByRole('heading', { name: 'Основное приложение' });
    await act(async () => { finish(json(200, { required: true })); });
    await waitFor(() => expect(screen.queryByRole('heading', { name: 'Создание администратора' })).not.toBeInTheDocument());
  });
});
