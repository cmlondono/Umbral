import type { AuthStatus, CatalogApp, Config, DeploySpec, History, HookURLs, Job, LivePoint, Range, Rule, Session, Snapshot } from './types';

/* Interfaz única entre el panel y el servidor. La implementación real habla
 * con el servicio de Umbral (Go) en el mismo origen. */
export interface Adapter {
  authStatus(): Promise<AuthStatus>;
  session(): Promise<Session>;
  login(user: string, password: string, remember: boolean, code?: string): Promise<Session>;
  totpBegin(password: string): Promise<{ secret: string; uri: string }>;
  totpConfirm(code: string): Promise<{ recovery: string[] }>;
  totpDisable(password: string, code: string): Promise<{ ok: boolean }>;
  logout(): Promise<void>;
  changePassword(current: string, next: string): Promise<Session>;

  state(): Promise<Snapshot>;
  refresh(network?: boolean): Promise<Snapshot>;
  history(range: Range): Promise<History>;
  live(): Promise<LivePoint>;
  catalog(): Promise<CatalogApp[]>;
  job(id: string): Promise<Job>;

  containerAction(name: string, action: 'start' | 'stop' | 'restart' | 'remove'): Promise<Snapshot>;
  pullImage(ref: string): Promise<Job>;
  removeImages(items: { id: string; ref: string }[]): Promise<Snapshot>;
  backupDatabase(container: string): Promise<{ path: string; sizeKB: number; database: string }>;
  deploy(spec: DeploySpec): Promise<Job>;
  checkUpdates(): Promise<Snapshot>;
  applyUpdate(name: string): Promise<Job>;
  revertUpdate(name: string): Promise<Job>;

  wakePc(): Promise<Snapshot>;
  checkPc(): Promise<Snapshot>;
  scanNetwork(): Promise<Snapshot>;
  updateDevice(mac: string, name: string, pinned: boolean): Promise<Snapshot>;
  saveConfig(c: Config): Promise<Snapshot>;
  saveRule(rule: Rule): Promise<{ id: string; hook: HookURLs | null; state: Snapshot }>;
  setRuleEnabled(id: string, enabled: boolean): Promise<Snapshot>;
  deleteRule(id: string): Promise<Snapshot>;
  runRule(id: string): Promise<Snapshot>;
  newHook(id: string): Promise<{ hook: HookURLs }>;
}


/** Error de la API con su código HTTP. 0 significa que el servidor no respondió. */
export class ApiError extends Error {
  constructor(message: string, public status: number, public data: Record<string, unknown> | null = null) {
    super(message);
  }
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  let res: Response;
  try {
    res = await fetch(path, {
      method,
      credentials: 'same-origin',
      headers: { 'X-Umbral': '1', ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}) },
      body: body !== undefined ? JSON.stringify(body) : undefined,
    });
  } catch {
    throw new ApiError('El servidor no responde.', 0);
  }
  const text = await res.text();
  let data: unknown = null;
  try { data = text ? JSON.parse(text) : null; } catch { /* respuesta no JSON */ }
  if (!res.ok) {
    const msg = (data as { error?: string } | null)?.error || (res.status >= 500 ? 'El servidor no responde.' : 'No se pudo completar la acción.');
    throw new ApiError(msg, res.status >= 502 && !data ? 0 : res.status, data as Record<string, unknown> | null);
  }
  return data as T;
}

const confirm = { confirm: true };

export const httpAdapter: Adapter = {
  authStatus: () => call('GET', '/api/auth/status'),
  session: async () => {
    const s = await call<Session & AuthStatus>('GET', '/api/auth/session');
    if (!s.user) throw new ApiError('Sin sesión', 401);
    return s;
  },
  login: (user, password, remember, code) => call('POST', '/api/auth/login', { user, password, remember, code: code || '' }),
  totpBegin: (password) => call('POST', '/api/auth/totp/begin', { password }),
  totpConfirm: (code) => call('POST', '/api/auth/totp/confirm', { code }),
  totpDisable: (password, code) => call('POST', '/api/auth/totp/disable', { password, code }),
  logout: () => call('POST', '/api/auth/logout', {}),
  changePassword: (current, next) => call('POST', '/api/auth/password', { current, next }),

  state: () => call('GET', '/api/state'),
  refresh: (network) => call('POST', '/api/refresh' + (network ? '?red=1' : ''), {}),
  history: (range) => call('GET', '/api/metrics/history?range=' + range),
  live: () => call('GET', '/api/metrics/live'),
  catalog: () => call('GET', '/api/catalog'),
  job: (id) => call('GET', '/api/jobs/' + encodeURIComponent(id)),

  containerAction: (name, action) => call('POST', `/api/containers/${encodeURIComponent(name)}/${action}`, action === 'start' ? {} : confirm),
  pullImage: (ref) => call('POST', '/api/images/pull', { ref }),
  removeImages: (items) => call('POST', '/api/images/remove', { items, confirm: true }),
  backupDatabase: (container) => call('POST', `/api/containers/${encodeURIComponent(container)}/backup`, {}),
  deploy: (spec) => call('POST', '/api/deploy', spec),
  checkUpdates: () => call('POST', '/api/updates/check', {}),
  applyUpdate: (name) => call('POST', `/api/updates/${encodeURIComponent(name)}/apply`, confirm),
  revertUpdate: (name) => call('POST', `/api/updates/${encodeURIComponent(name)}/revert`, confirm),

  wakePc: () => call('POST', '/api/pc/wake', {}),
  checkPc: () => call('POST', '/api/pc/check', {}),
  scanNetwork: () => call('POST', '/api/network/scan', {}),
  updateDevice: (mac, name, pinned) => call('PUT', '/api/network/devices/' + encodeURIComponent(mac), { name, pinned }),
  saveConfig: (c) => call('PUT', '/api/config', c),
  saveRule: (rule) => (rule.id ? call('PUT', '/api/automations/' + encodeURIComponent(rule.id), rule) : call('POST', '/api/automations', rule)),
  setRuleEnabled: (id, enabled) => call('POST', `/api/automations/${encodeURIComponent(id)}/enabled`, { enabled }),
  deleteRule: (id) => call('POST', `/api/automations/${encodeURIComponent(id)}/delete`, { confirm: true }),
  runRule: (id) => call('POST', `/api/automations/${encodeURIComponent(id)}/run`, {}),
  newHook: (id) => call('POST', `/api/automations/${encodeURIComponent(id)}/hook`, {}),
};

/* En desarrollo, VITE_UMBRAL_PRUEBAS=1 usa un servidor simulado en memoria
 * para revisar el diseño sin el servidor real. No se incluye en la versión
 * que se instala. */
export async function getAdapter(): Promise<Adapter> {
  if (import.meta.env.DEV && import.meta.env.VITE_UMBRAL_PRUEBAS === '1') {
    const m = await import('./dev/fixture');
    return m.fixtureAdapter;
  }
  return httpAdapter;
}
