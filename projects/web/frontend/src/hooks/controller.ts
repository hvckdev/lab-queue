import { ApiError, actionKey, request, type RequestOptions } from '../lib/api';
import type { CreateSessionInput, Entry, Group, LabQueueController, LoginInput, Notice, QueueSnapshot, RegisterInput, Session, Subject, User } from '../types';

type View = Pick<LabQueueController, 'user' | 'booting' | 'groups' | 'subjects' | 'sessions' | 'selectedId' | 'queue' | 'subjectFilter' | 'groupFilter' | 'loading' | 'busy' | 'authBusy' | 'authError' | 'bootstrapError' | 'syncError' | 'lastUpdated' | 'notice' | 'pending'>;
type Kind = 'create' | 'join' | 'call-next';
interface PendingAction { key: string; kind: Kind; sessionId: number | null; previousEntryId: number | undefined }

const initialView = (): View => ({
  user: null, booting: true, groups: [], subjects: [], sessions: [], selectedId: null, queue: null,
  subjectFilter: '', groupFilter: '', loading: false, busy: false, authBusy: false, authError: null,
  bootstrapError: null, syncError: null, lastUpdated: null, notice: null,
  pending: { join: false, callNext: false, create: false },
});
const message = (error: unknown) => error instanceof Error ? error.message : 'Не удалось выполнить запрос';
const groupsUnavailable = 'Не удалось загрузить учебные группы. Попробуйте ещё раз.';

// The controller owns asynchronous lifetimes. React subscribes to immutable
// snapshots; stale network replies can never replace the current account/queue.
export class LabQueueStore {
  private view = initialView();
  private listeners = new Set<() => void>();
  private active = false;
  private lifecycle = 0;
  private authEpoch = 0;
  private selectionVersion = 0;
  private readRun = 0;
  private reading = false;
  private readAgain = false;
  private readAbort: AbortController | null = null;
  private bootstrapAbort: AbortController | null = null;
  private polling: ReturnType<typeof setInterval> | null = null;
  private authRunning = false;
  private authAction = 0;
  private mutating = false;
  private mutationVersion = 0;
  private noticeId = 0;
  private pendingActions = new Map<string, PendingAction>();

  getSnapshot = (): View => this.view;
  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => { this.listeners.delete(listener); };
  };

  private emit(patch: Partial<View>): void {
    if (!this.active) return;
    this.view = { ...this.view, ...patch };
    this.listeners.forEach((listener) => listener());
  }

  private notify(text: string, tone: Notice['tone'] = 'success'): void {
    this.emit({ notice: { id: ++this.noticeId, message: text, tone } });
  }

  clearNotice = (): void => { this.emit({ notice: null }); };
  endExpiredSession = (userId: number): void => {
    if (this.active && this.view.user?.id === userId) this.clearSession(true);
  };

  private updatePending(): void {
    const actions = [...this.pendingActions.values()];
    this.emit({ pending: {
      join: actions.some((action) => action.kind === 'join' && action.sessionId === this.view.selectedId),
      callNext: actions.some((action) => action.kind === 'call-next' && action.sessionId === this.view.selectedId),
      create: actions.some((action) => action.kind === 'create'),
    } });
  }

  private invalidateRead(): void {
    this.readRun++;
    this.readAbort?.abort();
    this.readAbort = null;
    this.reading = false;
    this.readAgain = false;
  }

  private clearSession(expired = false): void {
    this.authEpoch++;
    this.selectionVersion++;
    this.authAction++;
    this.mutationVersion++;
    this.authRunning = false;
    this.mutating = false;
    this.bootstrapAbort?.abort();
    this.invalidateRead();
    this.pendingActions.clear();
    this.emit({ user: null, booting: false, subjects: [], sessions: [], selectedId: null, queue: null, subjectFilter: '', groupFilter: '',
      busy: false, loading: false, authBusy: false, authError: null, syncError: null, lastUpdated: null,
      pending: { join: false, callNext: false, create: false } });
    if (expired) this.notify('Сеанс истёк. Войдите снова.', 'error');
  }

  private async api<T>(path: string, options: RequestOptions = {}, protectedRequest = true): Promise<T> {
    const epoch = this.authEpoch;
    const userId = this.view.user?.id;
    try { return await request<T>(path, options); }
    catch (error) {
      // Selection changes do not hide expiry, but an earlier account's reply
      // cannot sign out a newly authenticated account.
      if (protectedRequest && error instanceof ApiError && error.status === 401 && userId && this.active && epoch === this.authEpoch && userId === this.view.user?.id) {
        this.clearSession(true);
      }
      throw error;
    }
  }

  start = (): void => {
    if (this.active) return;
    this.active = true;
    this.lifecycle++;
    this.authEpoch++;
    // An effect restart reuses this store, but none of the previous lifetime's
    // requests owns a visible busy flag anymore. Their completion is guarded
    // by the invalidated action versions; pending keys remain available.
    this.emit({ busy: false, authBusy: false, loading: false, authError: null });
    void this.bootstrap();
    this.polling = setInterval(() => { if (!document.hidden) void this.refresh(); }, 3000);
    document.addEventListener('visibilitychange', this.onVisibility);
  };

  stop = (): void => {
    this.active = false;
    this.lifecycle++;
    this.authEpoch++;
    this.authAction++;
    this.mutationVersion++;
    this.authRunning = false;
    this.mutating = false;
    this.bootstrapAbort?.abort();
    this.invalidateRead();
    if (this.polling) clearInterval(this.polling);
    this.polling = null;
    document.removeEventListener('visibilitychange', this.onVisibility);
  };

  private onVisibility = (): void => { if (!document.hidden) void this.refresh(); };

  private async bootstrap(): Promise<void> {
    const lifecycle = this.lifecycle;
    const epoch = this.authEpoch;
    this.bootstrapAbort?.abort();
    const controller = new AbortController();
    this.bootstrapAbort = controller;
    this.emit({ booting: true, bootstrapError: null });
    const [groups, identity] = await Promise.allSettled([
      this.api<{ groups: Group[] }>('/api/groups', { signal: controller.signal }, false),
      this.api<{ user: User }>('/api/auth/me', { signal: controller.signal }, false),
    ]);
    if (!this.active || lifecycle !== this.lifecycle || epoch !== this.authEpoch) return;
    let bootstrapError: string | null = null;
    if (groups.status === 'fulfilled') this.emit({ groups: groups.value.groups });
    else if (!(groups.reason instanceof ApiError && groups.reason.aborted)) bootstrapError = groupsUnavailable;
    if (identity.status === 'fulfilled') {
      this.emit({ bootstrapError });
      await this.enter(identity.value.user, epoch);
      return;
    }
    if (!(identity.reason instanceof ApiError && identity.reason.status === 401)) bootstrapError ||= message(identity.reason);
    this.emit({ booting: false, bootstrapError });
  }

  retryBootstrap = (): void => {
    if (!this.active || this.authRunning || this.mutating) return;
    this.authEpoch++;
    this.invalidateRead();
    void this.bootstrap();
  };

  private async enter(user: User, epoch: number): Promise<void> {
    if (!this.active || epoch !== this.authEpoch) return;
    const sameUser = this.view.user?.id === user.id;
    const selectedId = sameUser ? this.view.selectedId : null;
    const subjectFilter = sameUser ? this.view.subjectFilter : '';
    const groupFilter = user.role === 'student' ? String(user.group_id ?? '') : sameUser ? this.view.groupFilter : '';
    this.selectionVersion++;
    this.invalidateRead();
    // Revalidation after Fast Refresh is not a new account. Dropping an
    // uncertain create key here could turn its retry into a duplicate session.
    if (!sameUser) this.pendingActions.clear();
    this.emit({ user, booting: false, subjectFilter, groupFilter, busy: false, authBusy: this.authRunning,
      sessions: [], subjects: [], selectedId, queue: null, loading: true, authError: null, syncError: null,
      lastUpdated: sameUser ? this.view.lastUpdated : null });
    this.updatePending();
    await this.refresh();
  }

  private authenticate = async (path: string, input: LoginInput | RegisterInput): Promise<boolean> => {
    if (!this.active || this.authRunning || this.mutating) return false;
    this.authRunning = true;
    const action = ++this.authAction;
    const epoch = ++this.authEpoch;
    this.bootstrapAbort?.abort();
    this.invalidateRead();
    this.emit({ authBusy: true, authError: null });
    try {
      const result = await this.api<{ user: User }>(path, { method: 'POST', body: input }, false);
      if (!this.active || epoch !== this.authEpoch || action !== this.authAction) return false;
      await this.enter(result.user, epoch);
      return this.view.user?.id === result.user.id;
    } catch (error) {
      if (this.active && epoch === this.authEpoch && action === this.authAction) this.emit({ authError: message(error) });
      return false;
    } finally {
      if (action === this.authAction) { this.authRunning = false; this.emit({ authBusy: false }); }
    }
  };

  login = (input: LoginInput): Promise<boolean> => this.authenticate('/api/auth/login', input);
  register = (input: RegisterInput): Promise<boolean> => this.authenticate('/api/auth/register', input);

  logout = async (): Promise<boolean> => {
    if (!this.active || this.authRunning || this.mutating) return false;
    this.authRunning = true;
    const action = ++this.authAction;
    const epoch = ++this.authEpoch;
    this.invalidateRead();
    this.emit({ authBusy: true });
    try {
      await this.api('/api/auth/logout', { method: 'POST', body: {} });
      if (!this.active || epoch !== this.authEpoch || action !== this.authAction) return false;
      this.clearSession();
      return true;
    } catch (error) {
      if (this.active && epoch === this.authEpoch) this.notify(message(error), 'error');
      return false;
    } finally {
      if (action === this.authAction) { this.authRunning = false; this.emit({ authBusy: false }); }
    }
  };

  refresh = async (): Promise<void> => {
    const user = this.view.user;
    if (!this.active || this.view.booting || this.mutating) return;
    if (this.reading) { this.readAgain = true; return; }
    this.reading = true;
    const run = ++this.readRun;
    const epoch = this.authEpoch;
    const selection = this.selectionVersion;
    const controller = new AbortController();
    this.readAbort = controller;
    const current = () => this.active && run === this.readRun && epoch === this.authEpoch && selection === this.selectionVersion && user?.id === this.view.user?.id;
    const refreshGroups = async () => {
      try {
        const catalogue = await this.api<{ groups: Group[] }>('/api/groups', { signal: controller.signal }, false);
        if (!current()) return;
        this.emit({ groups: catalogue.groups, ...(this.view.bootstrapError === groupsUnavailable ? { bootstrapError: null } : {}) });
      } catch (error) {
        if (current() && !(error instanceof ApiError && error.aborted)) this.emit({ bootstrapError: groupsUnavailable });
      }
    };
    let catalogueError: string | null = null;
    try {
      if (user?.role === 'admin') {
        const identity = await this.api<{ user: User }>('/api/auth/me', { signal: controller.signal });
        if (!current()) return;
        if (identity.user.id !== user.id || identity.user.role !== user.role) {
          await this.enter(identity.user, epoch);
          return;
        }
        await refreshGroups();
        if (!current()) return;
        this.emit({ loading: false, syncError: null, lastUpdated: new Date(), sessions: [], selectedId: null, queue: null });
        return;
      }
      await refreshGroups();
      if (!current()) return;
      // The registration form stays mounted while public groups update. No
      // identity bootstrap or authenticated queue request is needed here.
      if (!user) {
        this.emit({ loading: false });
        return;
      }
      {
        try {
          const catalogue = await this.api<{ subjects: Subject[] }>('/api/subjects', { signal: controller.signal });
          if (!current()) return;
          this.emit({ subjects: catalogue.subjects });
        } catch (error) {
          if (!current() || error instanceof ApiError && error.aborted) return;
          catalogueError = message(error);
        }
      }
      const params = new URLSearchParams();
      if (this.view.subjectFilter) params.set('subject_id', this.view.subjectFilter);
      if (this.view.groupFilter) params.set('group_id', this.view.groupFilter);
      const sessions = await this.api<{ sessions: Session[] }>(`/api/sessions?${params}`, { signal: controller.signal });
      if (!current()) return;
      const selected = sessions.sessions.some((session) => session.id === this.view.selectedId) ? this.view.selectedId : sessions.sessions[0]?.id ?? null;
      this.emit({ sessions: sessions.sessions, selectedId: selected, ...(selected !== this.view.selectedId ? { queue: null } : {}) });
      this.updatePending();
      if (selected !== null) {
        const snapshot = await this.api<QueueSnapshot>(`/api/sessions/${selected}/queue`, { signal: controller.signal });
        if (!current() || selected !== this.view.selectedId) return;
        this.emit({ queue: snapshot });
        for (const [scope, pending] of this.pendingActions) {
          if (pending.sessionId !== selected) continue;
          const mine = snapshot.my_entry;
          if (pending.kind === 'join' && mine && (['waiting', 'called', 'completed'].includes(mine.status) || mine.id !== pending.previousEntryId)) this.pendingActions.delete(scope);
          if (pending.kind === 'call-next' && snapshot.entries.some((entry) => entry.status === 'called')) this.pendingActions.delete(scope);
        }
        this.updatePending();
      } else this.emit({ queue: null });
      this.emit({ loading: false, syncError: catalogueError, lastUpdated: new Date() });
    } catch (error) {
      if (current() && !(error instanceof ApiError && error.aborted)) this.emit({ loading: false, syncError: message(error) });
    } finally {
      if (run === this.readRun) {
        this.reading = false;
        this.readAbort = null;
        if (this.readAgain) { this.readAgain = false; void this.refresh(); }
      }
    }
  };

  setSubjectFilter = (value: string): void => {
    if (this.mutating || value === this.view.subjectFilter) return;
    this.selectionVersion++;
    this.invalidateRead();
    this.emit({ subjectFilter: value, selectedId: null, queue: null, loading: true });
    void this.refresh();
  };
  setGroupFilter = (value: string): void => {
    if (this.mutating || this.view.user?.role !== 'teacher' || value === this.view.groupFilter) return;
    this.selectionVersion++;
    this.invalidateRead();
    this.emit({ groupFilter: value, selectedId: null, queue: null, loading: true });
    void this.refresh();
  };
  selectSession = (id: number): void => {
    if (this.mutating || id === this.view.selectedId || !this.view.sessions.some((session) => session.id === id)) return;
    this.selectionVersion++;
    this.invalidateRead();
    this.emit({ selectedId: id, queue: null, loading: true });
    this.updatePending();
    void this.refresh();
  };

  private async mutate<T>(kind: string, path: string, body: unknown, keyed: boolean, success: (value: T) => void): Promise<T | null> {
    if (!this.active || this.view.booting || !this.view.user || this.view.user.role === 'admin' || this.authRunning || this.mutating) return null;
    this.mutating = true;
    const version = ++this.mutationVersion;
    const epoch = this.authEpoch;
    const scope = `${kind}:${path}:${JSON.stringify(body)}`;
    let pending = this.pendingActions.get(scope);
    if (keyed && !pending) {
      pending = { key: actionKey(), kind: kind as Kind, sessionId: this.view.selectedId, previousEntryId: this.view.queue?.my_entry?.id };
      this.pendingActions.set(scope, pending);
    }
    this.invalidateRead();
    this.emit({ busy: true });
    this.updatePending();
    try {
      const result = await this.api<T>(path, { method: 'POST', body, key: pending?.key });
      if (!this.active || version !== this.mutationVersion || epoch !== this.authEpoch) return null;
      this.pendingActions.delete(scope);
      this.updatePending();
      success(result);
      return result;
    } catch (error) {
      if (this.active && version === this.mutationVersion && epoch === this.authEpoch) {
        if (!(error instanceof ApiError && error.uncertain)) this.pendingActions.delete(scope);
        this.updatePending();
        if (!(error instanceof ApiError && error.aborted)) this.notify(message(error), 'error');
      }
      return null;
    } finally {
      if (version === this.mutationVersion && epoch === this.authEpoch) {
        this.mutating = false;
        this.emit({ busy: false });
        await this.refresh();
      }
    }
  }

  createSession = async (input: CreateSessionInput): Promise<Session | null> => {
    const result = await this.mutate<{ session: Session }>('create', '/api/sessions', input, true, ({ session }) => {
      this.selectionVersion++;
      this.emit({ subjectFilter: '', groupFilter: '', selectedId: session.id, queue: null, loading: true });
      this.notify('Сеанс создан. Студенты уже могут записываться.');
    });
    return result?.session ?? null;
  };
  joinQueue = async (): Promise<boolean> => {
    if (this.view.selectedId === null) return false;
    return (await this.mutate<{ entry: Entry }>('join', `/api/sessions/${this.view.selectedId}/entries`, {}, true, ({ entry }) => {
      this.notify(`Вы записаны. Талон №${entry.ticket_number}`);
    })) !== null;
  };
  callNext = async (): Promise<boolean> => {
    if (this.view.selectedId === null) return false;
    return (await this.mutate<{ entry: Entry | null }>('call-next', `/api/sessions/${this.view.selectedId}/call-next`, {}, true, ({ entry }) => {
      this.notify(entry ? `Вызван: ${entry.student_name}` : 'В очереди пока никого нет');
    })) !== null;
  };
  private entryAction = async (entryId: number, action: 'cancel' | 'complete' | 'skip', text: string): Promise<boolean> => {
    if (this.view.selectedId === null) return false;
    return (await this.mutate(action, `/api/sessions/${this.view.selectedId}/entries/${entryId}/${action}`, {}, false, () => { this.notify(text); })) !== null;
  };
  cancelEntry = (id: number): Promise<boolean> => this.entryAction(id, 'cancel', 'Ваша запись отменена');
  completeEntry = (id: number): Promise<boolean> => this.entryAction(id, 'complete', 'Сдача отмечена как завершённая');
  skipEntry = (id: number): Promise<boolean> => this.entryAction(id, 'skip', 'Студент пропущен');
  private sessionAction = async (action: 'close-registration' | 'finish', text: string): Promise<boolean> => {
    if (this.view.selectedId === null) return false;
    return (await this.mutate(action, `/api/sessions/${this.view.selectedId}/${action}`, {}, false, () => { this.notify(text); })) !== null;
  };
  closeRegistration = (): Promise<boolean> => this.sessionAction('close-registration', 'Новые записи закрыты');
  finishSession = (): Promise<boolean> => this.sessionAction('finish', 'Сеанс завершён');
}
