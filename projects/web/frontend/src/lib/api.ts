export class ApiError extends Error {
  readonly status: number | null;
  readonly uncertain: boolean;
  readonly aborted: boolean;
  readonly code: string | null;

  constructor(message: string, options: { status?: number; uncertain?: boolean; aborted?: boolean; code?: string } = {}) {
    super(message);
    this.name = 'ApiError';
    this.status = options.status ?? null;
    this.uncertain = options.uncertain ?? false;
    this.aborted = options.aborted ?? false;
    this.code = options.code ?? null;
  }
}

export interface RequestOptions {
  method?: 'GET' | 'POST';
  body?: unknown;
  key?: string;
  signal?: AbortSignal;
}

// This client never receives or persists a JWT; the browser owns the HttpOnly cookie.
export async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const controller = new AbortController();
  let timedOut = false;
  const abort = () => controller.abort();
  options.signal?.addEventListener('abort', abort, { once: true });
  if (options.signal?.aborted) controller.abort();
  const timer = setTimeout(() => { timedOut = true; controller.abort(); }, 15_000);
  try {
    let response: Response;
    try {
      response = await fetch(path, {
        method: options.method ?? 'GET',
        credentials: 'same-origin',
        headers: {
          Accept: 'application/json',
          ...(options.body === undefined ? {} : { 'Content-Type': 'application/json' }),
          ...(options.key ? { 'Idempotency-Key': options.key } : {}),
        },
        body: options.body === undefined ? undefined : JSON.stringify(options.body),
        signal: controller.signal,
      });
    } catch {
      if (options.signal?.aborted && !timedOut) throw new ApiError('Запрос отменён', { aborted: true });
      throw new ApiError('Связь прервалась. Действие могло сохраниться: проверьте очередь или повторите запрос.', { uncertain: true });
    }
    let data: unknown;
    try { data = await response.json(); }
    catch { throw new ApiError('Не удалось прочитать ответ сервера. Обновите очередь и попробуйте ещё раз.', { uncertain: true }); }
    if (!response.ok) {
      const error = data as { error?: { message?: string; code?: string } };
      throw new ApiError(error.error?.message || 'Не удалось выполнить запрос', { status: response.status, code: error.error?.code, uncertain: response.status >= 500 });
    }
    return data as T;
  } finally {
    clearTimeout(timer);
    options.signal?.removeEventListener('abort', abort);
  }
}

export function actionKey(): string {
  if (typeof crypto.randomUUID === 'function') return crypto.randomUUID();
  return Array.from(crypto.getRandomValues(new Uint8Array(16)), (value) => value.toString(16).padStart(2, '0')).join('');
}
