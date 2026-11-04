import { useEffect } from 'react';
import { Check, X } from 'lucide-react';
import { useLabQueue } from './hooks/useLabQueue';
import { AuthScreen } from './components/AuthScreen';
import { Dashboard } from './components/Dashboard';
import { Brand } from './components/Brand';
import { ProjectorScreen } from './components/ProjectorScreen';
import { dashboardURL, projectorSessionId } from './lib/projector';
import { SetupGate } from './components/SetupGate';
import { AdminDashboard } from './components/AdminDashboard';

export default function App() {
  return <SetupGate><AppRoutes /></SetupGate>;
}

function AppRoutes() {
  if (new URLSearchParams(window.location.search).has('display')) {
    const sessionId = projectorSessionId(window.location.search);
    return (
      <div data-theme="ulstu" className="app-root min-h-dvh font-sans text-base-content">
        {sessionId !== null ? <ProjectorScreen sessionId={sessionId} /> : (
          <main className="flex min-h-dvh flex-col items-center justify-center gap-6 p-8 text-center">
            <Brand />
            <h1 className="text-2xl font-bold">Не указан сеанс сдачи</h1>
            <p>Откройте очередь кнопкой «На проектор» в кабинете преподавателя.</p>
            <a className="btn btn-primary" href={dashboardURL(window.location.href)}>Открыть кабинет</a>
          </main>
        )}
      </div>
    );
  }
  return <DashboardApp />;
}

function DashboardApp() {
  const app = useLabQueue();

  useEffect(() => {
    if (!app.notice) return;
    const timer = window.setTimeout(app.clearNotice, app.notice.tone === 'error' ? 9000 : 6000);
    return () => window.clearTimeout(timer);
  }, [app.notice, app.clearNotice]);

  return (
    <div data-theme="ulstu" className="app-root min-h-dvh font-sans text-base-content">
      {app.booting ? (
        <main className="flex min-h-dvh flex-col items-center justify-center gap-8 p-6" aria-busy="true">
          <Brand />
          <div className="flex items-center gap-3 text-sm text-base-content/60" role="status">
            <span className="loading loading-spinner loading-sm" /> Открываем рабочее пространство
          </div>
        </main>
      ) : app.user ? app.user.role === 'admin' ? <AdminDashboard app={app} /> : <Dashboard app={app} /> : <AuthScreen app={app} />}

      {app.notice && (
        <div className={`notification alert ${app.notice.tone === 'error' ? 'notification-error' : 'notification-success'}`} role={app.notice.tone === 'error' ? 'alert' : 'status'}>
          {app.notice.tone === 'success' ? <Check size={19} aria-hidden="true" /> : <span className="font-mono font-bold" aria-hidden="true">!</span>}
          <span className="min-w-0 flex-1 text-sm leading-relaxed">{app.notice.message}</span>
          <button className="btn btn-ghost btn-square btn-xs shrink-0" aria-label="Закрыть уведомление" onClick={app.clearNotice}><X size={16} /></button>
        </div>
      )}
    </div>
  );
}
