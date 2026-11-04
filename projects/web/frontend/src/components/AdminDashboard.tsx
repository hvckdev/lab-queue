import { useState, type FormEvent } from 'react';
import { BookOpen, Check, ChevronLeft, ChevronRight, GraduationCap, LogOut, Plus, RefreshCw, ShieldCheck, Users } from 'lucide-react';
import type { LabQueueController, User } from '../types';
import { ADMIN_PAGE_SIZE, useAdminData } from '../hooks/useAdminData';
import { Brand } from './Brand';
import './AdminDashboard.css';

const roleNames: Record<User['role'], string> = { student: 'Студент', teacher: 'Преподаватель', admin: 'Администратор' };

export function AdminDashboard({ app }: { app: LabQueueController }) {
  const user = app.user;
  if (!user || user.role !== 'admin') return null;
  return <AdminWorkspace key={user.id} app={app} user={user} />;
}

function AdminWorkspace({ app, user }: { app: LabQueueController; user: User }) {
  const data = useAdminData(user, app.endExpiredSession);
  const [name, setName] = useState('');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [role, setRole] = useState<User['role']>('student');
  const [groupId, setGroupId] = useState('');
  const [subject, setSubject] = useState('');
  const [group, setGroup] = useState('');
  const [formError, setFormError] = useState<string | null>(null);
  const [groupErrorActive, setGroupErrorActive] = useState(false);
  const groupError = groupErrorActive ? data.error : null;
  const globalError = formError || (!groupErrorActive ? data.error : null) || app.syncError;

  function refreshLists() {
    setGroupErrorActive(false);
    data.refresh();
  }
  function changePage(offset: number) {
    setGroupErrorActive(false);
    data.setPageOffset(offset);
  }

  async function createUser(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setFormError(null);
    setGroupErrorActive(false);
    const bytes = new TextEncoder().encode(password).length;
    if (bytes < 8 || bytes > 72) { setFormError('Пароль должен содержать от 8 до 72 байт.'); return; }
    const success = await data.createUser({ name: name.trim(), email: email.trim(), password, role, group_id: role === 'student' ? Number(groupId) : null });
    if (success) { setName(''); setEmail(''); setPassword(''); setGroupId(''); }
  }
  async function createSubject(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setFormError(null);
    setGroupErrorActive(false);
    if (await data.createSubject(subject.trim())) setSubject('');
  }

  async function createGroup(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setFormError(null);
    setGroupErrorActive(true);
    if (await data.createGroup(group.trim())) {
      setGroup('');
      setGroupErrorActive(false);
    }
  }

  return <div className="min-h-dvh"><header className="admin-header"><Brand /><div className="admin-account"><div className="min-w-0 text-right"><p className="break-anywhere text-xs font-bold">{user.name}</p><p className="mt-1 text-[10px] text-base-content/65">Администратор</p></div><button className="btn btn-ghost btn-square" type="button" aria-label="Выйти из аккаунта" onClick={() => void app.logout()} disabled={data.busy || app.authBusy}><LogOut size={19} /></button></div></header><main className="admin-main"><div className="admin-title"><div><p className="eyebrow mb-3 flex items-center gap-2 text-primary"><ShieldCheck size={15} /> АДМИНИСТРАТОР</p><h1 className="page-title">Управление системой</h1></div><button className="btn btn-ghost gap-3 border border-base-300 bg-base-100 px-4" type="button" onClick={refreshLists} disabled={data.loading || data.busy}><RefreshCw size={16} className={data.loading ? 'animate-spin' : ''} />Обновить списки</button></div>{data.notice && <div className="alert alert-success mb-6 flex items-center gap-3" role="status"><Check size={18} />{data.notice}</div>}{globalError && <div className="alert alert-error mb-6" role="alert">{globalError}</div>}<div className="admin-columns"><section className="admin-section" aria-labelledby="admin-users-title"><header className="admin-section-header"><div><h2 id="admin-users-title"><Users size={21} />Пользователи</h2><p>{data.total} в системе</p></div></header><ul className="admin-users" aria-label="Пользователи" aria-busy={data.loading}>{data.loading && !data.users.length ? <li className="px-6 py-10 text-sm text-base-content/65">Загрузка пользователей…</li> : data.users.length ? data.users.map((item) => <li key={item.id}><div className="min-w-0"><p className="break-anywhere font-bold">{item.name}</p><p className="break-anywhere mt-1 text-xs text-base-content/70">{item.email}</p></div><div className="admin-user-meta"><span className="badge bg-info text-info-content">{roleNames[item.role]}</span><span>{item.group_id === null ? '—' : data.groups.find((group) => group.id === item.group_id)?.name || `Группа №${item.group_id}`}</span></div></li>) : <li className="px-6 py-10 text-sm text-base-content/65">Пользователи не найдены.</li>}</ul><footer className="admin-pagination"><span>{data.total ? `${data.offset + 1}–${Math.min(data.offset + data.users.length, data.total)} из ${data.total}` : '0 пользователей'}</span><div className="flex gap-2"><button className="btn btn-ghost btn-square btn-sm" type="button" aria-label="Предыдущая страница пользователей" onClick={() => changePage(data.offset - ADMIN_PAGE_SIZE)} disabled={data.loading || data.busy || data.offset === 0}><ChevronLeft size={18} /></button><button className="btn btn-ghost btn-square btn-sm" type="button" aria-label="Следующая страница пользователей" onClick={() => changePage(data.offset + ADMIN_PAGE_SIZE)} disabled={data.loading || data.busy || data.offset + ADMIN_PAGE_SIZE >= data.total}><ChevronRight size={18} /></button></div></footer></section><section className="admin-section" aria-labelledby="admin-create-user-title"><header className="admin-section-header"><h2 id="admin-create-user-title"><Plus size={21} />Создать пользователя</h2></header><form className="admin-form" onSubmit={createUser}>{app.bootstrapError && <div className="alert alert-warning flex flex-col items-start gap-2" role="alert"><p>{app.bootstrapError}</p><button type="button" className="font-bold underline underline-offset-4" onClick={app.retryBootstrap} disabled={data.busy}>Загрузить группы ещё раз</button></div>}<label className="form-field"><span>Имя и фамилия</span><input className="input w-full" value={name} onChange={(event) => setName(event.target.value)} autoComplete="off" maxLength={120} required disabled={data.busy} /></label><label className="form-field"><span>Email пользователя</span><input className="input w-full" type="email" value={email} onChange={(event) => setEmail(event.target.value)} autoComplete="off" maxLength={254} required disabled={data.busy} /></label><label className="form-field"><span>Пароль пользователя</span><input className="input w-full" type="password" aria-label="Пароль пользователя" value={password} onChange={(event) => setPassword(event.target.value)} autoComplete="new-password" minLength={8} maxLength={72} required disabled={data.busy} /><small className="text-xs font-normal leading-5 text-base-content/65">От 8 символов, не более 72 байт.</small></label><label className="form-field"><span>Роль</span><select className="select w-full" value={role} onChange={(event) => { setRole(event.target.value as User['role']); setGroupId(''); }} disabled={data.busy}><option value="student">Студент</option><option value="teacher">Преподаватель</option><option value="admin">Администратор</option></select></label>{role === 'student' && <label className="form-field"><span>Учебная группа</span><select className="select w-full" value={groupId} onChange={(event) => setGroupId(event.target.value)} required disabled={data.busy || !data.groups.length}><option value="">Выберите группу</option>{data.groups.map((group) => <option value={group.id} key={group.id}>{group.name}</option>)}</select></label>}<button className="btn btn-primary w-full justify-between px-4" type="submit" disabled={data.busy || role === 'student' && !data.groups.length}><span>{data.busy ? 'Сохранение…' : 'Создать пользователя'}</span><Plus size={18} /></button></form></section><section className="admin-section" aria-labelledby="admin-subjects-title"><header className="admin-section-header"><h2 id="admin-subjects-title"><BookOpen size={21} />Предметы</h2><span className="font-mono text-xs text-base-content/65">{data.subjects.length}</span></header><ul className="admin-subjects" aria-label="Предметы">{data.subjects.map((item) => <li className="break-anywhere" key={item.id}>{item.name}</li>)}{!data.subjects.length && <li className="text-base-content/65">{data.loading ? 'Загрузка предметов…' : 'Предметов пока нет.'}</li>}</ul></section><section className="admin-section" aria-labelledby="admin-create-subject-title"><header className="admin-section-header"><h2 id="admin-create-subject-title"><Plus size={21} />Добавить предмет</h2></header><form className="admin-form" onSubmit={createSubject}><label className="form-field"><span>Название предмета</span><input className="input w-full" value={subject} onChange={(event) => setSubject(event.target.value)} maxLength={200} required disabled={data.busy} /></label><button className="btn btn-primary w-full justify-between px-4" type="submit" disabled={data.busy || !subject.trim()}><span>Добавить предмет</span><Plus size={18} /></button></form></section><section className="admin-section" aria-labelledby="admin-groups-title"><header className="admin-section-header"><h2 id="admin-groups-title"><GraduationCap size={21} />Группы</h2><span className="font-mono text-xs text-base-content/65">{data.groups.length}</span></header><ul className="admin-subjects" aria-label="Группы">{data.groups.map((item) => <li className="break-anywhere" key={item.id}>{item.name}</li>)}{!data.groups.length && <li className="text-base-content/65">{data.loading ? 'Загрузка групп…' : 'Групп пока нет.'}</li>}</ul></section><section className="admin-section" aria-labelledby="admin-create-group-title"><header className="admin-section-header"><h2 id="admin-create-group-title"><Plus size={21} />Добавить группу</h2></header><form className="admin-form" onSubmit={createGroup}><label className="form-field"><span>Название группы</span><input className="input w-full" value={group} onChange={(event) => setGroup(event.target.value)} aria-invalid={Boolean(groupError)} aria-describedby={groupError ? 'admin-create-group-error' : undefined} maxLength={100} required disabled={data.busy} /></label>{groupError && <div id="admin-create-group-error" className="alert alert-error" role="alert">{groupError}</div>}<button className="btn btn-primary w-full justify-between px-4" type="submit" disabled={data.busy || !group.trim()}><span>Добавить группу</span><Plus size={18} /></button></form></section></div></main></div>;
}
