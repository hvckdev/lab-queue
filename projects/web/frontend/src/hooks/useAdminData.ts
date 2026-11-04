import { useCallback, useEffect, useRef, useState } from 'react';
import { ApiError, request } from '../lib/api';
import type { AdminUserInput, Group, Subject, User } from '../types';

export const ADMIN_PAGE_SIZE = 50;
interface AdminState {
  ownerId: number;
  offset: number;
  users: User[];
  total: number;
  subjects: Subject[];
  groups: Group[];
  loading: boolean;
  busy: boolean;
  error: string | null;
  notice: string | null;
}
const initial = (ownerId: number): AdminState => ({ ownerId, offset: 0, users: [], total: 0, subjects: [], groups: [], loading: true, busy: false, error: null, notice: null });

export function useAdminData(user: User, onSessionExpired: (userId: number) => void) {
  const [state, setState] = useState<AdminState>(() => initial(user.id));
  const [offset, setOffset] = useState(0);
  const [revision, setRevision] = useState(0);
  const active = useRef(false);
  const lifetime = useRef(0);
  const mutation = useRef(false);
  const read = useRef(0);
  const readAbort = useRef<AbortController | null>(null);
  const mutationAbort = useRef<AbortController | null>(null);
  const expired = useRef(onSessionExpired);
  expired.current = onSessionExpired;

  useEffect(() => {
    active.current = true;
    lifetime.current++;
    mutation.current = false;
    setState(initial(user.id));
    setOffset(0);
    return () => {
      active.current = false;
      lifetime.current++;
      read.current++;
      readAbort.current?.abort();
      mutationAbort.current?.abort();
    };
  }, [user.id]);

  const refresh = useCallback(() => {
    if (!mutation.current) setRevision((value) => value + 1);
  }, []);

  useEffect(() => {
    if (user.role !== 'admin') return;
    const life = lifetime.current;
    const run = ++read.current;
    const controller = new AbortController();
    readAbort.current?.abort();
    readAbort.current = controller;
    const current = () => active.current && life === lifetime.current && run === read.current;
    setState((previous) => ({ ...(previous.ownerId === user.id ? previous : initial(user.id)),
      ...(previous.offset !== offset ? { users: [] } : {}), offset, loading: true, error: null }));
    void Promise.all([
      request<{ users: User[]; total: number }>(`/api/admin/users?limit=${ADMIN_PAGE_SIZE}&offset=${offset}`, { signal: controller.signal }),
      request<{ subjects: Subject[] }>('/api/subjects', { signal: controller.signal }),
      request<{ groups: Group[] }>('/api/groups', { signal: controller.signal }),
    ]).then(([users, subjects, groups]) => {
      if (!current()) return;
      setState((previous) => ({ ...previous, ownerId: user.id, offset, users: users.users, total: users.total, subjects: subjects.subjects, groups: groups.groups, loading: false, error: null }));
    }).catch((failure: unknown) => {
      if (!current() || failure instanceof ApiError && failure.aborted) return;
      const denied = failure instanceof ApiError && (failure.status === 401 || failure.status === 403);
      if (failure instanceof ApiError && failure.status === 401) expired.current(user.id);
      setState((previous) => ({ ...previous, ...(denied ? { users: [], subjects: [], groups: [], total: 0 } : {}), loading: false,
        error: denied ? 'Доступ к разделу администратора недоступен. Войдите снова.' : 'Не удалось загрузить данные. Повторите обновление.' }));
    });
    return () => { read.current++; controller.abort(); };
  }, [user.id, user.role, offset, revision]);

  async function create<T>(path: string, body: unknown, notice: string, firstPage: boolean, apply?: (previous: AdminState, result: T) => Partial<AdminState>): Promise<boolean> {
    if (!active.current || mutation.current || user.role !== 'admin') return false;
    mutation.current = true;
    const life = lifetime.current;
    const controller = new AbortController();
    mutationAbort.current = controller;
    read.current++;
    readAbort.current?.abort();
    setState((previous) => ({ ...previous, busy: true, loading: false, error: null, notice: null }));
    const current = () => active.current && life === lifetime.current;
    try {
      const result = await request<T>(path, { method: 'POST', body, signal: controller.signal });
      if (!current()) return false;
      setState((previous) => ({ ...previous, ...apply?.(previous, result), notice }));
      if (firstPage) setOffset(0);
      setRevision((value) => value + 1);
      return true;
    } catch (failure) {
      if (!current() || failure instanceof ApiError && failure.aborted) return false;
      const denied = failure instanceof ApiError && (failure.status === 401 || failure.status === 403);
      if (failure instanceof ApiError && failure.status === 401) expired.current(user.id);
      setState((previous) => ({ ...previous, ...(denied ? { users: [], subjects: [], groups: [], total: 0 } : {}),
        error: failure instanceof ApiError && failure.uncertain ? 'Ответ сервера не получен. Обновите список, прежде чем повторять создание.' : failure instanceof Error ? failure.message : 'Не удалось сохранить данные.' }));
      return false;
    } finally {
      if (current()) { mutation.current = false; setState((previous) => ({ ...previous, busy: false })); }
    }
  }

  const scoped = state.ownerId === user.id ? state : initial(user.id);
  return {
    ...scoped,
    refresh,
    setPageOffset: (value: number) => { if (!mutation.current && !state.loading) setOffset(Math.max(0, value)); },
    createUser: (input: AdminUserInput) => create('/api/admin/users', input, 'Пользователь создан.', true),
    createSubject: (name: string) => create('/api/admin/subjects', { name }, 'Предмет добавлен.', false),
    createGroup: (name: string) => create<{ group: Group }>('/api/admin/groups', { name }, 'Группа добавлена.', false, (previous, { group }) => ({
      groups: [...previous.groups.filter((item) => item.id !== group.id), group].sort((a, b) => a.name.localeCompare(b.name, 'ru')),
    })),
  };
}
