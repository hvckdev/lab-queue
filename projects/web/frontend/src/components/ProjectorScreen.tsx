import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { BellRing, CalendarDays, ChevronLeft, ChevronRight, Clock3, ListOrdered, Maximize, Minimize, Monitor, RefreshCw, ShieldAlert, WifiOff } from 'lucide-react';
import { useQueueDisplay } from '../hooks/useQueueDisplay';
import { formatClock, formatInterval, participants, timeZone } from '../lib/format';
import { dashboardURL } from '../lib/projector';
import { Brand } from './Brand';
import './ProjectorScreen.css';

function pageSize(): number { return window.innerHeight < 850 ? 4 : 6; }

export function ProjectorScreen({ sessionId }: { sessionId: number }) {
  const { snapshot, loading, error, lastUpdated, access, refresh } = useQueueDisplay(sessionId);
  const [now, setNow] = useState(Date.now);
  const [page, setPage] = useState(0);
  const [perPage, setPerPage] = useState(pageSize);
  const [fullscreen, setFullscreen] = useState(Boolean(document.fullscreenElement));
  const [fullscreenMessage, setFullscreenMessage] = useState<string | null>(null);
  const waitingList = useRef<HTMLOListElement>(null);
  const rowMeasurements = useRef<HTMLDivElement>(null);
  const waitingPanel = useRef<HTMLElement>(null);
  const footer = useRef<HTMLElement>(null);

  useEffect(() => {
    const clock = window.setInterval(() => setNow(Date.now()), 1000);
    const changed = () => setFullscreen(Boolean(document.fullscreenElement));
    document.addEventListener('fullscreenchange', changed);
    return () => {
      window.clearInterval(clock);
      document.removeEventListener('fullscreenchange', changed);
    };
  }, []);

  const session = snapshot?.session;
  const called = snapshot?.entries.find((entry) => entry.status === 'called');
  const waiting = snapshot?.entries.filter((entry) => entry.status === 'waiting') ?? [];
  const pages = Math.max(1, Math.ceil(waiting.length / perPage));
  // Clamp during render as well as in the effect, so a shorter live queue can
  // never briefly leave the display on an empty, out-of-range page.
  const currentPage = Math.min(page, pages - 1);
  const visible = waiting.slice(currentPage * perPage, (currentPage + 1) * perPage);
  const stale = Boolean(snapshot && (error || !lastUpdated || now - lastUpdated.getTime() > 10_000));
  const beforeStart = session ? now < new Date(session.starts_at).getTime() : false;

  useEffect(() => { setPage(0); }, [sessionId]);
  useEffect(() => { setPage((value) => Math.min(value, pages - 1)); }, [pages, perPage]);
  useEffect(() => {
    if (pages <= 1) return;
    const rotation = window.setInterval(() => setPage((value) => (value + 1) % pages), 10_000);
    return () => window.clearInterval(rotation);
  }, [pages]);
  useEffect(() => {
    const previous = document.title;
    document.title = session ? `Очередь · ${session.group_name} — УлГТУ` : 'Очередь на проекторе — УлГТУ';
    return () => { document.title = previous; };
  }, [session?.group_name]);

  async function toggleFullscreen() {
    setFullscreenMessage(null);
    try {
      if (document.fullscreenElement) await document.exitFullscreen();
      else if (document.documentElement.requestFullscreen) await document.documentElement.requestFullscreen();
      else setFullscreenMessage('Полный экран недоступен. Разверните окно средствами браузера.');
    } catch {
      setFullscreenMessage('Браузер не разрешил полный экран. Разверните окно средствами браузера.');
    }
  }

  const allowed = access === 'allowed';
  const syncText = access === 'login-required' ? 'Требуется вход' : access === 'forbidden' ? 'Нет доступа' : stale ? 'Данные устарели' : lastUpdated && allowed ? `Обновлено в ${formatClock(lastUpdated)}` : error ? 'Нет подключения' : 'Подключение';

  useLayoutEffect(() => {
    const list = waitingList.current;
    const measurements = rowMeasurements.current;
    const panel = waitingPanel.current;
    if (!list || !measurements || !panel) return;
    const fit = () => {
      // Measure every row at the actual table width. A later page with a long
      // name must fit too, so use the tallest row for the whole queue.
      const maxRow = Math.max(0, ...Array.from(measurements.children, (row) => row.getBoundingClientRect().height));
      const top = list.getBoundingClientRect().top;
      if (!maxRow || !top) return;
      const footerHeight = (footer.current?.getBoundingClientRect().height ?? 22) + 42;
      const paginationHeight = waiting.length > 1 ? 82 : 0;
      setPerPage(Math.max(1, Math.floor((window.innerHeight - top - footerHeight - paginationHeight) / maxRow)));
    };
    fit();
    const observer = typeof ResizeObserver === 'function' ? new ResizeObserver(fit) : null;
    observer?.observe(measurements);
    Array.from(measurements.children).forEach((row) => observer?.observe(row));
    window.addEventListener('resize', fit);
    return () => { observer?.disconnect(); window.removeEventListener('resize', fit); };
  }, [snapshot, allowed, stale]);

  return (
    <div className="projector-screen">
      <header className="projector-toolbar">
        <Brand />
        <div className="projector-tools">
          <div className="projector-sync" role="status">
            <span className={`projector-sync-dot ${stale || error ? 'is-stale' : ''}`} />
            <span>{syncText}</span>
          </div>
          <button type="button" className="btn btn-ghost projector-fullscreen" onClick={() => void toggleFullscreen()}>
            {fullscreen ? <Minimize size={19} /> : <Maximize size={19} />}
            <span>{fullscreen ? 'Выйти из полного экрана' : 'Во весь экран'}</span>
          </button>
        </div>
      </header>

      {fullscreenMessage && <p className="projector-fullscreen-message" role="status">{fullscreenMessage}</p>}

      {!allowed || !snapshot ? (
        <main className="projector-message">
          {loading || access === 'checking' && !error ? <span className="loading loading-spinner loading-lg" aria-hidden="true" /> : access === 'forbidden' ? <ShieldAlert size={48} strokeWidth={1.5} /> : <Monitor size={48} strokeWidth={1.5} />}
          <h1>{access === 'login-required' ? 'Войдите как преподаватель' : access === 'forbidden' ? 'Очередь недоступна' : error ? 'Не удалось загрузить очередь' : 'Загрузка очереди'}</h1>
          <p>{access === 'login-required' ? 'Откройте основную страницу и войдите в аккаунт преподавателя. Это окно подключится автоматически.' : access === 'forbidden' ? 'Окно проектора доступно преподавателю, создавшему этот сеанс.' : error || 'Данные появятся после подключения к серверу.'}</p>
          <div className="projector-message-actions">
            {(access === 'login-required' || access === 'forbidden') && <a className="btn btn-primary" href={dashboardURL(window.location.href)} target="_blank" rel="noopener noreferrer">Открыть основную страницу</a>}
            {(error || access !== 'checking') && <button type="button" className="btn btn-ghost" onClick={refresh} disabled={loading}><RefreshCw size={18} />Повторить</button>}
          </div>
        </main>
      ) : (
        <main className="projector-content">
          <section className="projector-session" aria-label="Сеанс сдачи">
            <div className="projector-session-heading">
              <div className="projector-session-subject"><span>Очередь на сдачу</span><h1>{snapshot.session.subject_name}</h1></div>
              <span className="projector-group">{snapshot.session.group_name}</span>
            </div>
            <h2>{snapshot.session.lab_title}</h2>
            <p className="projector-interval"><CalendarDays size={18} aria-hidden="true" /><span>{formatInterval(snapshot.session.starts_at, snapshot.session.ends_at)} · {timeZone}</span></p>
            <p className={`projector-session-state ${snapshot.session.finished ? 'is-finished' : ''}`}>{snapshot.session.finished ? 'Сеанс завершён' : !snapshot.session.registration_open ? 'Запись закрыта · очередь продолжает обслуживаться' : beforeStart ? 'Запись открыта · вызов начнётся по расписанию' : 'Запись открыта'}</p>
          </section>

          {stale && <div className="projector-offline" role="alert"><WifiOff size={24} aria-hidden="true" /><div><strong>Нет актуального обновления</strong><p>Показана последняя полученная очередь. Вызов мог измениться. {lastUpdated && `Последнее обновление: ${formatClock(lastUpdated)}.`}</p></div><button type="button" className="btn btn-ghost" onClick={refresh} disabled={loading}><RefreshCw size={18} />Обновить</button></div>}

          <div className="projector-board">
            <section className={`projector-called ${called ? 'has-student' : ''} ${(called?.student_name.length ?? 0) > 50 ? 'has-long-name' : ''}`} aria-label="Текущий вызов" aria-live="polite" aria-atomic="true">
              <div className="projector-called-label">{called ? <BellRing size={25} /> : <Clock3 size={25} />}<span>{called ? 'Сейчас на сдаче' : snapshot.session.finished ? 'Приём завершён' : beforeStart ? 'До начала приёма' : 'Сейчас никто не вызван'}</span></div>
              {called ? <><div className="projector-ticket-label">Талон</div><p className="projector-ticket">№ {called.ticket_number}</p><h2 className="projector-called-name">{called.student_name}</h2><p className="projector-called-note">Подойдите к преподавателю</p></> : <><p className="projector-ticket projector-ticket-empty" aria-hidden="true">—</p><h2 className="projector-empty-title">{snapshot.session.finished ? 'Сеанс завершён' : beforeStart ? `Начало в ${formatClock(snapshot.session.starts_at)}` : waiting.length ? 'Ожидайте вызова' : 'Очередь пуста'}</h2><p className="projector-called-note">{snapshot.session.finished ? 'Ожидающие записи отменены.' : beforeStart ? 'Преподаватель начнёт вызывать студентов в указанное время.' : waiting.length ? 'Преподаватель вызовет следующего студента.' : 'Студенты появятся после записи в очередь.'}</p></>}
            </section>

            <section ref={waitingPanel} className="projector-waiting" aria-labelledby="projector-waiting-title">
              <header className="projector-waiting-heading"><h2 id="projector-waiting-title"><ListOrdered size={24} aria-hidden="true" />Ожидают вызова</h2><span>{participants(waiting.length)}</span></header>
              {waiting.length ? <><div className="projector-table-heading" aria-hidden="true"><span>Место</span><span>Студент</span><span>Талон</span></div><ol ref={waitingList} className="projector-entries" aria-label="Ожидающие студенты">{visible.map((entry) => <li className="projector-entry" key={entry.id} value={entry.position}><span className="projector-entry-position" aria-label={`Место ${entry.position}`}>{entry.position}</span><span className="projector-entry-name">{entry.student_name}</span><span className="projector-entry-ticket" aria-label={`Талон ${entry.ticket_number}`}>№ {entry.ticket_number}</span></li>)}</ol><div ref={rowMeasurements} className="projector-row-measurements" aria-hidden="true">{waiting.map((entry) => <div className="projector-entry" key={entry.id}><span className="projector-entry-position">{entry.position}</span><span className="projector-entry-name">{entry.student_name}</span><span className="projector-entry-ticket">№ {entry.ticket_number}</span></div>)}</div></> : <div className="projector-no-waiting"><ListOrdered size={38} strokeWidth={1.4} /><p>{snapshot.session.finished ? 'Ожидающих записей нет' : 'Ожидающих студентов нет'}</p></div>}
              {pages > 1 && <footer className="projector-pagination"><span>Страница {currentPage + 1} из {pages}<small>Автосмена каждые 10 секунд</small></span><div><button type="button" className="btn btn-ghost btn-square" aria-label="Предыдущая страница очереди" onClick={() => setPage((currentPage + pages - 1) % pages)}><ChevronLeft size={22} /></button><button type="button" className="btn btn-ghost btn-square" aria-label="Следующая страница очереди" onClick={() => setPage((currentPage + 1) % pages)}><ChevronRight size={22} /></button></div></footer>}
            </section>
          </div>
          <footer ref={footer} className="projector-footer"><span>Очередь обновляется каждые 3 секунды</span><time>{new Intl.DateTimeFormat('ru-RU', { hour: '2-digit', minute: '2-digit', second: '2-digit' }).format(now)}</time></footer>
        </main>
      )}
    </div>
  );
}
