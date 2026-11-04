import { MonitorUp } from 'lucide-react';
import { projectorURL } from '../lib/projector';

export function ProjectorLink({ sessionId }: { sessionId: number }) {
  const href = projectorURL(sessionId, window.location.href);

  return (
    <a
      className="btn btn-ghost w-full justify-between border border-base-300"
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      aria-label="На проектор — открыть очередь в отдельном окне"
      onClick={(event) => {
        if (event.button !== 0 || event.ctrlKey || event.metaKey || event.shiftKey || event.altKey) return;
        let popup: Window | null = null;
        try {
          popup = window.open('about:blank', '_blank', 'popup,width=1440,height=900');
          if (!popup) return;
          // Detach before navigating. Keeping a handle lets blocked popups fall
          // back to the normal link, including browsers with tab-only windows.
          popup.opener = null;
          popup.location.replace(href);
          event.preventDefault();
        } catch {
          popup?.close();
        }
      }}
    >
      <span>На проектор</span>
      <MonitorUp size={17} aria-hidden="true" />
    </a>
  );
}
