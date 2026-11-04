import type { Session } from '../types';

export function SessionStatus({ session }: { session: Session }) {
  if (session.finished) return <span className="badge status-finished">Завершён</span>;
  if (!session.registration_open) return <span className="badge status-closed">Запись закрыта</span>;
  if (new Date(session.starts_at) > new Date()) return <span className="badge status-soon">Скоро</span>;
  return <span className="badge status-open">Запись открыта</span>;
}
