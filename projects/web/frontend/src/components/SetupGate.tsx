import { useCallback, useEffect, useRef, useState, type FormEvent, type ReactNode } from 'react';
import { ArrowRight, LockKeyhole, RefreshCw } from 'lucide-react';
import { ApiError, request } from '../lib/api';
import type { User } from '../types';
import { Brand } from './Brand';

type Stage = 'checking' | 'required' | 'complete' | 'error';

export function SetupGate({ children }: { children: ReactNode }) {
  const [stage, setStage] = useState<Stage>('checking');
  const [error, setError] = useState<string | null>(null);
  const active = useRef(false);
  const generation = useRef(0);
  const abort = useRef<AbortController | null>(null);

  const check = useCallback(async (): Promise<boolean> => {
    const run = ++generation.current;
    abort.current?.abort();
    const controller = new AbortController();
    abort.current = controller;
    setStage('checking');
    setError(null);
    try {
      const status = await request<{ required: boolean }>('/api/setup', { signal: controller.signal });
      if (!active.current || run !== generation.current) return false;
      if (typeof status.required !== 'boolean') throw new Error('Некорректный ответ сервера');
      setStage(status.required ? 'required' : 'complete');
      return !status.required;
    } catch (failure) {
      if (active.current && run === generation.current && !(failure instanceof ApiError && failure.aborted)) {
        setError('Не удалось проверить настройку системы. Проверьте подключение и повторите запрос.');
        setStage('error');
      }
      return false;
    }
  }, []);

  useEffect(() => {
    active.current = true;
    void check();
    return () => { active.current = false; generation.current++; abort.current?.abort(); };
  }, [check]);

  if (stage === 'complete') return children;
  return <main className="auth-shell flex min-h-dvh items-start justify-center px-5 py-8 sm:py-12"><section className="auth-paper w-full min-w-0 max-w-lg rounded-2xl border border-base-300 px-6 py-8 sm:px-10 sm:py-10"><Brand />{stage === 'required' ? <SetupForm onComplete={() => setStage('complete')} onRecheck={check} /> : <div className="py-10"><h1 className="text-2xl font-extrabold tracking-tight">{stage === 'error' ? 'Система недоступна' : 'Проверка настройки'}</h1>{stage === 'checking' ? <p className="mt-6 flex items-center gap-3 text-sm text-base-content/70" role="status"><span className="loading loading-spinner loading-sm" /> Подключаемся к серверу</p> : <><p className="mt-4 text-sm leading-7 text-base-content/75" role="alert">{error}</p><button type="button" className="btn btn-primary mt-6 gap-3" onClick={() => void check()}><RefreshCw size={17} />Повторить</button></>}</div>}</section></main>;
}

function SetupForm({ onComplete, onRecheck }: { onComplete(): void; onRecheck(): Promise<boolean> }) {
  const [name, setName] = useState('');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [confirmation, setConfirmation] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const submitting = useRef(false);
  const active = useRef(false);
  const lifetime = useRef(0);
  const abort = useRef<AbortController | null>(null);
  useEffect(() => {
    active.current = true;
    lifetime.current++;
    return () => { active.current = false; lifetime.current++; abort.current?.abort(); };
  }, []);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (submitting.current) return;
    setError(null);
    if (password !== confirmation) { setError('Пароли не совпадают.'); return; }
    const bytes = new TextEncoder().encode(password).length;
    if (bytes < 8 || bytes > 72) { setError('Пароль должен содержать от 8 до 72 байт.'); return; }
    submitting.current = true;
    setBusy(true);
    const run = lifetime.current;
    const controller = new AbortController();
    abort.current = controller;
    const current = () => active.current && lifetime.current === run;
    try {
      const result = await request<{ user: User }>('/api/setup', { method: 'POST', body: { name: name.trim(), email: email.trim(), password }, signal: controller.signal });
      if (!current()) return;
      if (result.user?.role !== 'admin') throw new ApiError('Сервер не подтвердил создание администратора', { uncertain: true });
      setPassword('');
      setConfirmation('');
      onComplete();
    } catch (failure) {
      if (!current() || failure instanceof ApiError && failure.aborted) return;
      if (failure instanceof ApiError && (failure.status === 409 && failure.code === 'setup_completed' || failure.uncertain)) {
        // Another first-run request may have won, or its response was lost.
        // Re-read the permanent setup flag; never automatically repeat a POST.
        setPassword('');
        setConfirmation('');
        await onRecheck();
      } else setError(failure instanceof Error ? failure.message : 'Не удалось создать администратора.');
    } finally {
      submitting.current = false;
      if (current()) setBusy(false);
    }
  }

  return <div className="pt-9"><p className="eyebrow mb-3 text-primary">ПЕРВЫЙ ЗАПУСК</p><h1 className="text-2xl font-extrabold tracking-tight">Создание администратора</h1><p className="mt-4 text-sm leading-7 text-base-content/75">Задайте данные для управления пользователями и предметами. Этот шаг выполняется один раз.</p><form className="mt-7 space-y-5" onSubmit={submit}><label className="form-field"><span>Имя администратора</span><input className="input w-full" autoComplete="name" value={name} onChange={(event) => setName(event.target.value)} maxLength={120} required disabled={busy} /></label><label className="form-field"><span>Email администратора</span><input className="input w-full" type="email" autoComplete="username" value={email} onChange={(event) => setEmail(event.target.value)} maxLength={254} required disabled={busy} /></label><label className="form-field"><span>Пароль администратора</span><input className="input w-full" type="password" aria-label="Пароль администратора" autoComplete="new-password" value={password} onChange={(event) => setPassword(event.target.value)} minLength={8} maxLength={72} required disabled={busy} /><small className="text-xs font-normal leading-5 text-base-content/65">От 8 символов, не более 72 байт.</small></label><label className="form-field"><span>Повторите пароль</span><input className="input w-full" type="password" autoComplete="new-password" value={confirmation} onChange={(event) => setConfirmation(event.target.value)} required disabled={busy} /></label>{error && <div className="alert alert-error" role="alert">{error}</div>}<button type="submit" className="btn btn-primary w-full justify-between px-5" disabled={busy}><span>{busy ? 'Создание…' : 'Создать администратора'}</span>{busy ? <span className="loading loading-spinner loading-xs" /> : <ArrowRight size={18} />}</button><p className="flex items-start gap-2 text-xs leading-6 text-base-content/65"><LockKeyhole size={15} className="mt-1 shrink-0" />Пароль не хранится в открытом виде.</p></form></div>;
}
