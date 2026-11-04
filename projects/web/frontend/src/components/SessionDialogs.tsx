import { useEffect, useState, type FormEvent } from 'react';
import { ArrowRight, Clock3, Info, TriangleAlert } from 'lucide-react';
import type { CreateSessionInput, LabQueueController } from '../types';
import { localInput, timeZone } from '../lib/format';
import { Modal } from './Modal';

export interface Confirmation { kind: 'cancel' | 'skip' | 'finish'; sessionID: number; entryID?: number; name?: string }

export function CreateSessionDialog({ open, onClose, app }: { open: boolean; onClose(): void; app: LabQueueController }) {
  const [subject, setSubject] = useState('');
  const [group, setGroup] = useState('');
  const [title, setTitle] = useState('');
  const [starts, setStarts] = useState(() => localInput(new Date()));
  const [ends, setEnds] = useState(() => localInput(new Date(Date.now() + 2 * 60 * 60 * 1000)));
  const [error, setError] = useState<string | null>(null);
  const [attempted, setAttempted] = useState(false);
  const [frozenCreatePayload, setFrozenCreatePayload] = useState<CreateSessionInput | null>(null);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!submitting && !app.busy && !app.pending.create) setFrozenCreatePayload(null);
  }, [submitting, app.busy, app.pending.create, frozenCreatePayload]);

  useEffect(() => {
    if (!open) return;
    if (!subject && app.subjects[0]) setSubject(String(app.subjects[0].id));
    if (!group && app.groups[0]) setGroup(String(app.groups[0].id));
    setError(null);
    setAttempted(false);
  }, [open, app.subjects, app.groups, subject, group]);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (submitting || app.busy || (app.pending.create && !frozenCreatePayload)) return;
    let payload = frozenCreatePayload;
    if (!payload) {
      const start = new Date(starts);
      const end = new Date(ends);
      if (!title.trim()) { setError('Укажите название лабораторной работы.'); return; }
      if (!Number.isFinite(start.getTime()) || !Number.isFinite(end.getTime()) || end <= start) { setError('Окончание приёма должно быть позже начала.'); return; }
      payload = { subject_id: Number(subject), group_id: Number(group), lab_title: title.trim(), starts_at: start.toISOString(), ends_at: end.toISOString() };
      setFrozenCreatePayload(payload);
    }
    setError(null);
    setAttempted(true);
    setSubmitting(true);
    app.clearNotice();
    try {
      const created = await app.createSession(payload);
      if (created) { setFrozenCreatePayload(null); onClose(); setTitle(''); setStarts(localInput(new Date())); setEnds(localInput(new Date(Date.now() + 2 * 60 * 60 * 1000))); }
    } finally { setSubmitting(false); }
  }

  const fieldsLocked = app.busy || submitting || app.pending.create;
  const displayedError = error || (attempted && app.notice?.tone === 'error' ? app.notice.message : null);
  return <Modal open={open} onClose={onClose} busy={app.busy || submitting} eyebrow="НОВЫЙ СЕАНС" title="Создание сеанса" wide><form onSubmit={submit} className="space-y-6"><p className="text-xs leading-7 text-base-content/55">Укажите работу и время приёма. Студенты вашей группы смогут записаться сразу после создания сеанса.</p><div className="grid gap-5 sm:grid-cols-2"><label className="form-field"><span>Предмет</span><select className="select w-full" value={subject} onChange={(event) => setSubject(event.target.value)} required disabled={fieldsLocked}><option value="" disabled>Выберите предмет</option>{app.subjects.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label><label className="form-field"><span>Учебная группа</span><select className="select w-full" value={group} onChange={(event) => setGroup(event.target.value)} required disabled={fieldsLocked}><option value="" disabled>Выберите группу</option>{app.groups.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}</select></label><label className="form-field sm:col-span-2"><span className="flex justify-between gap-3">Лабораторная работа<small className="font-mono text-[9px] font-normal text-base-content/35">{title.length}/200</small></span><input className="input w-full" value={title} onChange={(event) => setTitle(event.target.value)} maxLength={200} required placeholder="Например: №3. Работа с базой данных" disabled={fieldsLocked} /></label><label className="form-field"><span>Начало приёма</span><input className="input w-full" type="datetime-local" value={starts} onChange={(event) => setStarts(event.target.value)} required disabled={fieldsLocked} /></label><label className="form-field"><span>Окончание приёма</span><input className="input w-full" type="datetime-local" value={ends} onChange={(event) => setEnds(event.target.value)} required disabled={fieldsLocked} /></label></div><div className="flex items-start gap-3 rounded-xl border border-base-300 bg-base-200 p-4 text-[11px] leading-6 text-base-content/60"><Info size={16} className="mt-1 shrink-0" /><p>Окончание закроет новые записи. Студентов, которые уже в очереди, можно принять и позже.</p></div>{app.pending.create && !submitting && !app.busy && <div className="alert border border-base-300 bg-base-200 text-base-content/70"><Info size={17} className="shrink-0" /><span>Проверяем результат предыдущей отправки. Поля временно заблокированы, чтобы не создать второй сеанс. Нажмите «Проверить создание».</span></div>}{displayedError && <div className="alert alert-error" role="alert"><TriangleAlert size={17} className="shrink-0" /><span className="break-anywhere">{displayedError}</span></div>}<div className="flex flex-col gap-4 border-t border-base-300 pt-5 sm:flex-row sm:items-center sm:justify-between"><span className="flex items-center gap-1.5 font-mono text-[9px] text-base-content/40"><Clock3 size={12} /> {timeZone}</span><button type="submit" className="btn btn-primary justify-between gap-8" disabled={app.busy || submitting || app.subjects.length === 0 || app.groups.length === 0 || (app.pending.create && !frozenCreatePayload)}><span>{app.pending.create ? 'Проверить создание' : 'Открыть запись'}</span>{app.busy ? <span className="loading loading-spinner loading-xs" /> : <ArrowRight size={18} />}</button></div></form></Modal>;
}

export function ConfirmationDialog({ confirmation, onClose, app }: { confirmation: Confirmation | null; onClose(): void; app: LabQueueController }) {
  const [attempted, setAttempted] = useState(false);
  useEffect(() => setAttempted(false), [confirmation]);
  const kind = confirmation?.kind;
  const changed = confirmation !== null && app.selectedId !== confirmation.sessionID;
  const title = kind === 'cancel' ? 'Отменить вашу запись?' : kind === 'skip' ? 'Пропустить студента?' : 'Завершить сеанс?';
  const label = kind === 'cancel' ? 'Отменить запись' : kind === 'skip' ? 'Пропустить' : 'Завершить сеанс';

  async function confirm() {
    if (!confirmation || changed) return;
    setAttempted(true);
    app.clearNotice();
    const success = confirmation.kind === 'finish' ? await app.finishSession() : confirmation.kind === 'cancel' && confirmation.entryID ? await app.cancelEntry(confirmation.entryID) : confirmation.kind === 'skip' && confirmation.entryID ? await app.skipEntry(confirmation.entryID) : false;
    if (success) onClose();
  }

  return <Modal open={confirmation !== null} onClose={onClose} busy={app.busy} eyebrow="ПОДТВЕРЖДЕНИЕ ДЕЙСТВИЯ" title={title}><div className="mb-5 flex h-12 w-12 items-center justify-center rounded-xl bg-error text-error-content"><TriangleAlert size={23} strokeWidth={1.5} /></div>{confirmation?.name && <p className="break-anywhere mb-3 text-sm font-bold">{confirmation.name}</p>}<p className="text-sm leading-7 text-base-content/60">{kind === 'cancel' ? 'Текущее место освободится. Если захотите записаться снова, получите новый талон в конце очереди.' : kind === 'skip' ? 'Текущая запись будет отмечена как пропущенная. Студент сможет записаться заново в конец очереди.' : 'Новые записи прекратятся, а все ожидающие записи будут отменены. Это действие нельзя отменить.'}</p>{changed && <div className="alert alert-warning mt-5">Выбранный сеанс изменился. Закройте окно и повторите действие в нужном сеансе.</div>}{attempted && app.notice?.tone === 'error' && <div className="alert alert-error mt-5" role="alert">{app.notice.message}</div>}<div className="mt-8 flex flex-col-reverse gap-3 sm:flex-row sm:justify-end"><button className="btn btn-ghost border border-base-300" onClick={onClose} disabled={app.busy}>Вернуться</button><button className="btn btn-secondary" onClick={() => void confirm()} disabled={app.busy || changed}>{app.busy && <span className="loading loading-spinner loading-xs" />}{label}</button></div></Modal>;
}
