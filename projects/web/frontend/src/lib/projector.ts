export function projectorSessionId(search: string): number | null {
  const value = new URLSearchParams(search).get('display');
  if (!value || !/^[1-9]\d*$/.test(value)) return null;
  const id = Number(value);
  return Number.isSafeInteger(id) ? id : null;
}

export function projectorURL(sessionId: number, currentURL: string): string {
  const url = new URL(currentURL);
  url.search = '';
  url.hash = '';
  url.searchParams.set('display', String(sessionId));
  return url.href;
}

export function dashboardURL(currentURL: string): string {
  const url = new URL(currentURL);
  url.search = '';
  url.hash = '';
  return url.href;
}
