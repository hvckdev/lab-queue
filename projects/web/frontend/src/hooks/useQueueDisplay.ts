import { useCallback, useEffect, useRef, useState } from 'react';
import { ApiError, request } from '../lib/api';
import type { QueueSnapshot, User } from '../types';

export interface QueueDisplayState {
  snapshot: QueueSnapshot | null;
  loading: boolean;
  error: string | null;
  lastUpdated: Date | null;
  access: 'checking' | 'allowed' | 'login-required' | 'forbidden';
}

interface ScopedState extends QueueDisplayState { sessionId: number }

const initialState = (sessionId: number): ScopedState => ({
  sessionId, snapshot: null, loading: true, error: null, lastUpdated: null, access: 'checking',
});

// A display stays pinned to one session and only reads the existing protected API.
// Its lifetime is independent of the teacher's selection in the control window.
export function useQueueDisplay(sessionId: number): QueueDisplayState & { refresh(): void } {
  const [state, setState] = useState<ScopedState>(() => initialState(sessionId));
  const refreshCurrent = useRef<() => void>(() => {});
  const refresh = useCallback(() => { refreshCurrent.current(); }, []);

  useEffect(() => {
    let active = true;
    let inFlight = false;
    let ownerId: number | null = null;
    let controller: AbortController | null = null;
    setState(initialState(sessionId));

    function update(patch: Partial<QueueDisplayState>): void {
      if (active) setState((previous) => ({ ...previous, ...patch, sessionId }));
    }

    async function read(): Promise<void> {
      if (!active || inFlight) return;
      inFlight = true;
      controller = new AbortController();
      const signal = controller.signal;
      update({ loading: true });
      try {
        const { user } = await request<{ user: User }>('/api/auth/me', { signal });
        if (!active) return;
        if (user.role !== 'teacher') {
          ownerId = null;
          update({ snapshot: null, lastUpdated: null, access: 'forbidden', error: 'Режим проектора доступен только преподавателям.' });
          return;
        }
        if (ownerId !== user.id) {
          ownerId = user.id;
          // A different cookie identity must not inherit the previous owner's data
          // while its own queue request is pending or fails.
          update({ snapshot: null, lastUpdated: null, access: 'checking', error: null });
        }
        const snapshot = await request<QueueSnapshot>(`/api/sessions/${sessionId}/queue`, { signal });
        if (!active) return;
        if (snapshot.session.id !== sessionId || snapshot.session.teacher_id !== user.id) {
          throw new ApiError('Этот сеанс недоступен для показа.', { status: 403 });
        }
        update({ snapshot, access: 'allowed', error: null, lastUpdated: new Date() });
      } catch (error) {
        if (!active || error instanceof ApiError && error.aborted) return;
        if (error instanceof ApiError && (error.status === 401 || error.status === 403)) {
          const login = error.status === 401;
          if (login) ownerId = null;
          update({ snapshot: null, lastUpdated: null, access: login ? 'login-required' : 'forbidden',
            error: login ? 'Войдите в аккаунт преподавателя в основном окне.' : 'Этот сеанс недоступен для показа.' });
        } else if (error instanceof ApiError && error.status === 404) {
          update({ snapshot: null, lastUpdated: null, access: 'allowed', error: 'Сеанс не найден.' });
        } else {
          // request() also serves mutations. Use a read-specific message here;
          // the display has no action whose outcome could be uncertain.
          update({ error: 'Не удалось обновить очередь. Повторяем подключение каждые 3 секунды.' });
        }
      } finally {
        inFlight = false;
        controller = null;
        update({ loading: false });
      }
    }

    const trigger = () => { void read(); };
    const onVisibility = () => { if (!document.hidden) trigger(); };
    refreshCurrent.current = trigger;
    trigger();
    const timer = window.setInterval(trigger, 3000);
    window.addEventListener('focus', trigger);
    window.addEventListener('online', trigger);
    document.addEventListener('visibilitychange', onVisibility);

    return () => {
      active = false;
      controller?.abort();
      refreshCurrent.current = () => {};
      window.clearInterval(timer);
      window.removeEventListener('focus', trigger);
      window.removeEventListener('online', trigger);
      document.removeEventListener('visibilitychange', onVisibility);
    };
  }, [sessionId]);

  // Effects run after render: never expose the old session for even one frame.
  return { ...(state.sessionId === sessionId ? state : initialState(sessionId)), refresh };
}
