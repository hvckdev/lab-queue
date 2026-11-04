import { useState, type FormEvent } from 'react';
import { ArrowDownRight, ArrowRight, ChevronDown, Eye, EyeOff, GraduationCap, LockKeyhole, Mail, RefreshCw, UserRound } from 'lucide-react';
import type { LabQueueController } from '../types';
import { Brand } from './Brand';

export function AuthScreen({ app }: { app: LabQueueController }) {
  const [mode, setMode] = useState<'login' | 'register'>('login');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [name, setName] = useState('');
  const [groupID, setGroupID] = useState('');
  const [showPassword, setShowPassword] = useState(false);
  const [submittedMode, setSubmittedMode] = useState<'login' | 'register' | null>(null);
  const registering = mode === 'register';

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setSubmittedMode(mode);
    if (registering) await app.register({ name: name.trim(), email: email.trim(), password, group_id: Number(groupID) });
    else await app.login({ email: email.trim(), password });
  }

  function demo(role: 'teacher' | 'student') {
    setMode('login');
    setSubmittedMode(null);
    setEmail(`${role}@example.com`);
    setPassword(role === 'teacher' ? 'Teacher123!' : 'Student123!');
  }

  return (
    <main className="auth-shell flex min-h-dvh justify-center px-5 py-8 sm:py-12">
      <section className="auth-paper flex w-full min-w-0 max-w-lg flex-col rounded-2xl border border-base-300 px-6 py-8 sm:px-10 sm:py-10">
        <Brand />
        <div className="mx-auto w-full max-w-md flex-1 py-9">
          <h1 className="text-2xl font-extrabold tracking-tight">{registering ? 'Регистрация студента' : 'Вход в систему'}</h1>

          <div className="tabs tabs-box auth-tabs mt-9 mb-7" role="tablist" aria-label="Вход или регистрация">
            <button type="button" className={`tab flex-1 ${!registering ? 'tab-active' : ''}`} role="tab" aria-selected={!registering} aria-controls="auth-form" onClick={() => { setMode('login'); setSubmittedMode(null); }} disabled={app.authBusy}>Войти</button>
            <button type="button" className={`tab flex-1 ${registering ? 'tab-active' : ''}`} role="tab" aria-selected={registering} aria-controls="auth-form" onClick={() => { setMode('register'); setSubmittedMode(null); }} disabled={app.authBusy}>Создать аккаунт</button>
          </div>

          <form id="auth-form" onSubmit={submit} className="space-y-5">
            {registering && <label className="form-field"><span>Имя и фамилия</span><div className="input w-full"><UserRound size={17} className="opacity-45" aria-hidden="true" /><input value={name} onChange={(event) => setName(event.target.value)} autoComplete="name" maxLength={120} placeholder="Как вас зовут?" required disabled={app.authBusy} /></div></label>}
            <label className="form-field"><span>Email</span><div className="input w-full"><Mail size={17} className="opacity-45" aria-hidden="true" /><input type="email" value={email} onChange={(event) => setEmail(event.target.value)} autoComplete="username" maxLength={254} placeholder="you@example.com" required disabled={app.authBusy} /></div></label>
            <label className="form-field"><span className="flex items-center justify-between gap-3">Пароль{registering && <small className="font-normal text-base-content/45">не менее 8 символов</small>}</span><div className="input w-full"><LockKeyhole size={17} className="opacity-45" aria-hidden="true" /><input type={showPassword ? 'text' : 'password'} value={password} onChange={(event) => setPassword(event.target.value)} autoComplete={registering ? 'new-password' : 'current-password'} minLength={registering ? 8 : undefined} maxLength={72} placeholder="Ваш пароль" required disabled={app.authBusy} /><button type="button" className="password-toggle" aria-label={showPassword ? 'Скрыть пароль' : 'Показать пароль'} aria-pressed={showPassword} onClick={() => setShowPassword(!showPassword)}>{showPassword ? <EyeOff size={17} /> : <Eye size={17} />}</button></div></label>
            {registering && <label className="form-field"><span>Учебная группа</span><select className="select w-full" value={groupID} onChange={(event) => setGroupID(event.target.value)} required disabled={app.authBusy || app.groups.length === 0}><option value="">Выберите свою группу</option>{app.groups.map((group) => <option key={group.id} value={group.id}>{group.name}</option>)}</select><small className="text-xs font-normal leading-5 text-base-content/50">Вам будут доступны сеансы выбранной группы.</small></label>}

            {app.bootstrapError && <div className="alert auth-alert items-start text-sm" role="alert"><RefreshCw size={17} className="mt-0.5 shrink-0" /><div className="min-w-0"><p>{app.bootstrapError}</p><button type="button" className="mt-2 underline underline-offset-4" onClick={app.retryBootstrap}>Загрузить группы ещё раз</button></div></div>}
            {app.authError && submittedMode === mode && <div className="alert alert-error text-sm" role="alert"><span>{app.authError}</span></div>}
            <button className="btn btn-primary h-12 w-full justify-between px-5" type="submit" disabled={app.authBusy || (registering && app.groups.length === 0)}><span>{app.authBusy ? 'Загрузка…' : registering ? 'Создать аккаунт студента' : 'Войти'}</span>{app.authBusy ? <span className="loading loading-spinner loading-xs" /> : <ArrowRight size={19} />}</button>
          </form>

          <details className="demo-details mt-7 border-t border-base-300 pt-5">
            <summary className="flex cursor-pointer list-none items-center justify-between gap-3 text-xs font-semibold text-base-content/60"><span>Демонстрационные аккаунты</span><ChevronDown size={15} className="demo-chevron" /></summary>
            <div className="mt-4 grid grid-cols-2 gap-3">
              <button type="button" className="demo-button" onClick={() => demo('teacher')} disabled={app.authBusy}><GraduationCap size={18} /><strong>Преподаватель</strong><span>Управлять приёмом</span><ArrowDownRight size={15} className="demo-arrow" /></button>
              <button type="button" className="demo-button" onClick={() => demo('student')} disabled={app.authBusy}><UserRound size={18} /><strong>Студент</strong><span>Запись в очередь</span><ArrowDownRight size={15} className="demo-arrow" /></button>
            </div>
            <p className="mt-3 text-[11px] leading-5 text-base-content/45">Кнопка заполнит email и пароль. Затем нажмите «Войти».</p>
          </details>
        </div>
        <footer className="flex justify-between gap-3 border-t border-base-300 pt-5 text-[10px] font-medium text-base-content/45"><span>УлГТУ</span><span className="font-mono">{new Date().getFullYear()}</span></footer>
      </section>
    </main>
  );
}
