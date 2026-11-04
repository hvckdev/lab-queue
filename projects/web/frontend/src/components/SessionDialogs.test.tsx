import { act, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeAll, describe, expect, it, vi } from 'vitest';
import type { LabQueueController, Session } from '../types';
import { CreateSessionDialog } from './SessionDialogs';

beforeAll(() => {
  if (typeof HTMLDialogElement.prototype.showModal !== 'function') {
    Object.defineProperty(HTMLDialogElement.prototype, 'showModal', { configurable: true, value(this: HTMLDialogElement) { this.setAttribute('open', ''); } });
  }
  if (typeof HTMLDialogElement.prototype.close !== 'function') {
    Object.defineProperty(HTMLDialogElement.prototype, 'close', { configurable: true, value(this: HTMLDialogElement) { this.removeAttribute('open'); } });
  }
});

function controller(createSession: LabQueueController['createSession']): LabQueueController {
  return {
    user: { id: 1, name: 'Преподаватель', email: 'teacher@example.com', role: 'teacher', group_id: null },
    booting: false, groups: [{ id: 1, name: 'ИВТ-101' }, { id: 2, name: 'ИВТ-102' }], subjects: [{ id: 1, name: 'Программирование' }, { id: 2, name: 'Базы данных' }],
    sessions: [], selectedId: null, queue: null, subjectFilter: '', groupFilter: '', loading: false, busy: false, authBusy: false,
    authError: null, bootstrapError: null, syncError: null, lastUpdated: null, notice: null, pending: { join: false, callNext: false, create: false },
    setSubjectFilter: vi.fn(), setGroupFilter: vi.fn(), selectSession: vi.fn(), clearNotice: vi.fn(), endExpiredSession: vi.fn(), retryBootstrap: vi.fn(), refresh: vi.fn(async () => {}),
    login: vi.fn(async () => true), register: vi.fn(async () => true), logout: vi.fn(async () => true), createSession,
    joinQueue: vi.fn(async () => true), callNext: vi.fn(async () => true), closeRegistration: vi.fn(async () => true), finishSession: vi.fn(async () => true),
    cancelEntry: vi.fn(async () => true), completeEntry: vi.fn(async () => true), skipEntry: vi.fn(async () => true),
  };
}

function fillForm() {
  fireEvent.change(screen.getByLabelText('Предмет'), { target: { value: '2' } });
  fireEvent.change(screen.getByLabelText('Учебная группа'), { target: { value: '2' } });
  fireEvent.change(screen.getByLabelText(/Лабораторная работа/), { target: { value: 'Транзакции и блокировки' } });
  fireEvent.change(screen.getByLabelText('Начало приёма'), { target: { value: '2030-01-20T10:00' } });
  fireEvent.change(screen.getByLabelText('Окончание приёма'), { target: { value: '2030-01-20T12:00' } });
}

describe('CreateSessionDialog uncertain result', () => {
  it('locks the form across close/reopen and retries exactly the first payload', async () => {
    let resolveFirst!: (session: Session | null) => void;
    const first = new Promise<Session | null>((resolve) => { resolveFirst = resolve; });
    const created: Session = { id: 22, teacher_id: 1, subject_id: 2, subject_name: 'Базы данных', group_id: 2, group_name: 'ИВТ-102', lab_title: 'Транзакции и блокировки', starts_at: '2030-01-20T10:00:00Z', ends_at: '2030-01-20T12:00:00Z', registration_closed: false, registration_open: true, finished: false, active_count: 0 };
    const create = vi.fn<LabQueueController['createSession']>().mockReturnValueOnce(first).mockResolvedValueOnce(created);
    const close = vi.fn();
    let app = controller(create);
    const view = render(<CreateSessionDialog open onClose={close} app={app} />);
    fillForm();
    fireEvent.click(screen.getByRole('button', { name: 'Открыть запись' }));
    expect(create).toHaveBeenCalledTimes(1);
    const original = create.mock.calls[0][0];
    app = { ...app, busy: true, pending: { ...app.pending, create: true } };
    view.rerender(<CreateSessionDialog open onClose={close} app={app} />);
    await act(async () => { resolveFirst(null); await first; });
    app = { ...app, busy: false };
    view.rerender(<CreateSessionDialog open onClose={close} app={app} />);

    for (const label of ['Предмет', 'Учебная группа', /Лабораторная работа/, 'Начало приёма', 'Окончание приёма']) expect(screen.getByLabelText(label)).toBeDisabled();
    expect(screen.getByText(/Поля временно заблокированы/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Проверить создание' })).toBeEnabled();
    view.rerender(<CreateSessionDialog open={false} onClose={close} app={app} />);
    view.rerender(<CreateSessionDialog open onClose={close} app={app} />);
    expect(screen.getByLabelText(/Лабораторная работа/)).toHaveValue('Транзакции и блокировки');
    expect(screen.getByLabelText('Начало приёма')).toHaveValue('2030-01-20T10:00');
    expect(screen.getByLabelText('Предмет')).toHaveValue('2');
    expect(screen.getByLabelText(/Лабораторная работа/)).toBeDisabled();

    fireEvent.click(screen.getByRole('button', { name: 'Проверить создание' }));
    await waitFor(() => expect(create).toHaveBeenCalledTimes(2));
    expect(create.mock.calls[1][0]).toEqual(original);
    await waitFor(() => expect(close).toHaveBeenCalledTimes(1));
    app = { ...app, pending: { ...app.pending, create: false } };
    view.rerender(<CreateSessionDialog open onClose={close} app={app} />);
    expect(screen.getByLabelText(/Лабораторная работа/)).toBeEnabled();
    expect(screen.getByLabelText(/Лабораторная работа/)).toHaveValue('');
  });

  it('releases a definitely rejected request so a corrected form can be submitted', async () => {
    const create = vi.fn<LabQueueController['createSession']>().mockResolvedValue(null);
    const app = controller(create);
    render(<CreateSessionDialog open onClose={vi.fn()} app={app} />);
    fillForm();
    fireEvent.click(screen.getByRole('button', { name: 'Открыть запись' }));
    await waitFor(() => expect(screen.getByLabelText(/Лабораторная работа/)).toBeEnabled());
    fireEvent.change(screen.getByLabelText(/Лабораторная работа/), { target: { value: 'Исправленная лабораторная' } });
    fireEvent.click(screen.getByRole('button', { name: 'Открыть запись' }));
    await waitFor(() => expect(create).toHaveBeenCalledTimes(2));
    expect(create.mock.calls[1][0].lab_title).toBe('Исправленная лабораторная');
  });
});
