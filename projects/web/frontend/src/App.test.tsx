import { fireEvent, render, screen } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import App from './App';
import { useLabQueue } from './hooks/useLabQueue';
import { ProjectorLink } from './components/ProjectorLink';

vi.mock('./hooks/useLabQueue', () => ({ useLabQueue: vi.fn() }));
vi.mock('./components/ProjectorScreen', () => ({
  ProjectorScreen: ({ sessionId }: { sessionId: number }) => <h1>Очередь сеанса {sessionId}</h1>,
}));

beforeEach(() => {
  vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true, status: 200, json: async () => ({ required: false }) })));
});
afterEach(() => { window.history.replaceState({}, '', '/'); vi.unstubAllGlobals(); });

describe('projector window routing', () => {
  it('opens the exact requested session without starting the dashboard store', async () => {
    window.history.replaceState({}, '', '/?display=72');
    render(<App />);
    expect(await screen.findByRole('heading', { name: 'Очередь сеанса 72' })).toBeInTheDocument();
    expect(useLabQueue).not.toHaveBeenCalled();
  });

  it.each(['', '0', '-1', '1.5', 'abc', '9007199254740993'])('rejects invalid session id %j without querying a default queue', async (value) => {
    window.history.replaceState({}, '', `/?display=${value}`);
    render(<App />);
    expect(await screen.findByRole('heading', { name: 'Не указан сеанс сдачи' })).toBeInTheDocument();
    expect(useLabQueue).not.toHaveBeenCalled();
  });

  it('opens a separate window for the selected session and keeps its link usable', () => {
    window.history.replaceState({}, '', '/?other=filter#sessions');
    const popup = { opener: window, location: { replace: vi.fn() }, close: vi.fn() };
    const open = vi.spyOn(window, 'open').mockReturnValue(popup as unknown as Window);
    const view = render(<ProjectorLink sessionId={72} />);
    const link = screen.getByRole('link', { name: /На проектор/ });
    expect(link).toHaveAttribute('href', 'http://localhost:8080/?display=72');
    expect(link).toHaveAttribute('rel', 'noopener noreferrer');
    fireEvent.click(link);
    expect(open).toHaveBeenCalledWith('about:blank', '_blank', expect.stringContaining('popup'));
    expect(popup.opener).toBeNull();
    expect(popup.location.replace).toHaveBeenCalledWith('http://localhost:8080/?display=72');

    view.rerender(<ProjectorLink sessionId={73} />);
    expect(link).toHaveAttribute('href', 'http://localhost:8080/?display=73');
  });

  it('keeps the native link when the browser blocks a popup', () => {
    vi.spyOn(window, 'open').mockReturnValue(null);
    render(<ProjectorLink sessionId={72} />);
    expect(fireEvent.click(screen.getByRole('link', { name: /На проектор/ }))).toBe(true);
  });
});
