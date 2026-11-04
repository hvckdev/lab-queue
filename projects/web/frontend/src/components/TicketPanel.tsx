import { ArrowRight, BellRing, CheckCheck, CircleSlash, Ticket, X } from 'lucide-react';
import type { Entry, LabQueueController, QueueSnapshot } from '../types';

function placeClass(position: number) { return position > 99999 ? 'ticket-place ticket-place-tiny' : position > 999 ? 'ticket-place ticket-place-compact' : 'ticket-place'; }
function number(position: number) { return String(position).padStart(2, '0'); }

export function TicketPanel({ app, snapshot, onCancel }: { app: LabQueueController; snapshot: QueueSnapshot | null; onCancel(entry: Entry): void }) {
  if (!snapshot) return <div className="rounded-2xl border border-base-300 p-6"><Ticket size={24} className="mb-5 text-base-content/35" /><p className="text-sm font-bold">Здесь будет ваш талон</p><p className="mt-2 text-xs leading-6 text-base-content/50">Выберите сеанс сдачи, чтобы посмотреть свободное место.</p></div>;

  const { session, my_entry: entry, next_position: next } = snapshot;
  const waiting = entry?.status === 'waiting';
  const called = entry?.status === 'called';
  const completed = entry?.status === 'completed';
  const canJoin = session.registration_open && !session.finished;

  if (completed) return <div className="admission-ticket completed-ticket"><div className="ticket-body"><p className="eyebrow flex items-center justify-between">СДАЧА ЗАВЕРШЕНА<CheckCheck size={18} /></p><div className="mt-10 mb-7 flex h-16 w-16 items-center justify-center rounded-full border border-current/20"><CheckCheck size={32} strokeWidth={1.5} /></div><h3 className="text-2xl font-extrabold tracking-tight">Сдача завершена</h3><p className="mt-4 text-xs leading-6 opacity-75">Преподаватель завершил вашу сдачу. Повторная запись в этот сеанс недоступна.</p></div><div className="ticket-tear" /><div className="ticket-bottom flex items-center justify-between font-mono text-xs"><span className="text-[9px] uppercase opacity-75">ВАШ ТАЛОН</span><strong>№ {entry.ticket_number}</strong></div></div>;

  if (waiting || called) return (
    <>
      <div className={`admission-ticket ${called ? 'called-ticket' : ''}`}>
        <div className="ticket-body">
          <p className="eyebrow flex items-center justify-between gap-2">{called ? 'ВЫЗОВ НА СДАЧУ' : 'ВАШЕ МЕСТО'}{called ? <BellRing size={18} /> : <Ticket size={18} />}</p>
          <p className={placeClass(entry.position)}>{number(entry.position)}</p>
          <h3 className="text-xl font-extrabold tracking-tight">{called ? 'Вас вызывают' : 'Вы в очереди'}</h3>
          <p className="mt-2 text-xs leading-6 opacity-75">{called ? 'Подойдите к преподавателю. Теперь ваша очередь сдавать работу.' : 'Талон закреплён. Мы покажем, когда преподаватель вас вызовет.'}</p>
        </div>
        <div className="ticket-tear" />
        <div className="ticket-bottom">
          <div className="mb-5 flex items-center justify-between gap-4"><div><p className="eyebrow opacity-75">НОМЕР ТАЛОНА</p><strong className="mt-1 block font-mono text-base">№ {entry.ticket_number}</strong></div><div className="text-right"><p className="eyebrow opacity-75">ПЕРЕД ВАМИ</p><strong className="mt-1 block font-mono text-base">{Math.max(0, entry.position - 1)}</strong></div></div>
          {waiting ? <button className="btn btn-outline ticket-button border-base-content/25 bg-transparent" onClick={() => onCancel(entry)} disabled={app.busy}><span>Отменить запись</span><X size={16} /></button> : <div className="border-t border-white/20 pt-4 text-[10px] leading-5 opacity-75">После сдачи преподаватель отметит её завершение.</div>}
        </div>
      </div>
      <p className="mt-4 px-1 text-[10px] leading-6 text-base-content/45">Номер талона постоянный. Место уменьшается, когда очередь движется.</p>
    </>
  );

  if (!canJoin && !app.pending.join) return <div className="admission-ticket closed-ticket"><div className="ticket-body"><p className="eyebrow flex items-center justify-between">{session.finished ? 'СЕАНС ЗАВЕРШЁН' : 'ЗАПИСЬ ЗАКРЫТА'}<CircleSlash size={18} /></p><div className="mt-10 mb-6 font-mono text-6xl leading-none">—</div><h3 className="text-xl font-extrabold tracking-tight">{session.finished ? 'Сеанс завершён' : 'Запись закрыта'}</h3><p className="mt-3 text-xs leading-6 opacity-75">{session.finished ? 'Этот сеанс сдачи завершён. Посмотрите другие доступные сеансы.' : 'Преподаватель принимает тех, кто уже успел записаться.'}</p></div></div>;

  return <div className="admission-ticket dark-ticket"><div className="ticket-body"><p className="eyebrow flex items-center justify-between text-white/75">МЕСТО ПРИ ЗАПИСИ<Ticket size={18} /></p><p className={`${placeClass(next)} text-primary`}>{number(next)}</p><h3 className="text-xl font-extrabold tracking-tight">Запись в очередь</h3><p className="mt-3 text-xs leading-6 text-white/75">{entry?.status === 'skipped' ? 'Вас пропустили. Можно записаться заново — в конец очереди.' : entry?.status === 'cancelled' ? 'Прошлая запись отменена. При желании займите новое место.' : 'После записи вы получите талон в конце очереди.'}</p></div><div className="ticket-tear" /><div className="ticket-bottom"><button className="btn btn-primary ticket-button" onClick={() => void app.joinQueue()} disabled={app.busy}><span>{app.pending.join ? 'Проверить запись' : 'Встать в очередь'}</span>{app.busy ? <span className="loading loading-spinner loading-xs" /> : <ArrowRight size={18} />}</button><p className="mt-4 text-[10px] leading-5 text-white/75">Очередь меняется. Точное место появится после записи.</p></div></div>;
}
