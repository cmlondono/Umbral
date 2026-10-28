import { useSyncExternalStore } from 'react';
import { ApiError, type Adapter } from '../data/adapters';
import type { CatalogApp, History, Job, LivePoint, Range, Snapshot } from '../data/types';

export type Page = 'home' | 'devices' | 'server' | 'services' | 'network' | 'remote' | 'auto' | 'settings';
export type SetTab = 'ui' | 'devices' | 'integrations' | 'notify' | 'security' | 'pending' | 'about';

export interface Prefs {
  theme: 'light' | 'dark' | 'auto';
  density: 'comfortable' | 'compact';
  wallpaper: 'ambient' | 'plain';
  transparency: 'normal' | 'reduced';
  autoRefresh: 'off' | '30s' | '1m' | '5m';
}

export interface ModalSpec {
  title: string;
  body: string;
  confirmLabel: string;
  confirmCls?: string;
  noCancel?: boolean;
  secondaryLabel?: string;
  onSecondary?: () => void;
  onConfirm: () => void;
}

export interface Toast { id: string; tone: 'ok' | 'info' | 'warn' | 'err'; title: string; body: string }

export interface WizState {
  step: 1 | 2 | 3 | 4;
  source: 'catalog' | 'compose' | 'image';
  app: string | null;
  compose: string;
  parsed: ParsedCompose | null;
  image: string;
  cport: string;
  name: string;
  port: string;
  access: 'lan' | 'tailscale';
  restart: 'always' | 'never';
  memLimit: 'none' | '256' | '512' | '1024';
  initKey: string;
  jobId: string | null;
  job: Job | null;
  submitErr: string | null;
}

export type ParsedCompose = { err: string } | { err?: undefined; services: string[]; images: string[]; ports: [string, string][] };

export interface PullState { phase: 'form' | 'pulling' | 'done' | 'error'; name: string; ref: string; job: Job | null; existed: boolean }

export interface State {
  phase: 'loading' | 'login' | 'app';
  authConfigured: boolean;
  user: string;
  totp: boolean;
  recoveryLeft: number;
  snap: Snapshot | null;
  online: boolean;
  lastOk: number;
  page: Page;
  moreOpen: boolean;
  drawer: { type: 'device' | 'service' | 'image'; id: string } | null;
  modal: ModalSpec | null;
  toasts: Toast[];
  refreshing: boolean;
  prefs: Prefs;
  svcTab: 'containers' | 'images' | 'updates' | 'avail';
  svcFilter: 'all' | 'running' | 'issues' | 'stopped';
  search: string;
  imgFilter: 'all' | 'used' | 'unused';
  imgSearch: string;
  netFilter: 'all' | 'connected' | 'noReply';
  setTab: SetTab;
  range: Range;
  hist: Partial<Record<Range, History>>;
  busy: Record<string, 'restarting' | 'stopping' | 'starting' | 'removing'>;
  checkingPc: boolean;
  scanning: boolean;
  wiz: WizState | null;
  pull: PullState | null;
  logs: string | null;
  catalog: CatalogApp[] | null;
  updJobs: Record<string, string>;
  updView: Record<string, Job>;
  busyBackup: boolean;
  toolDraft: string | null;
  editingTool: boolean;
}

const PREFS_KEY = 'umbral.preferencias';
function loadPrefs(): Prefs {
  const def: Prefs = { theme: 'auto', density: 'comfortable', wallpaper: 'ambient', transparency: 'normal', autoRefresh: '30s' };
  try { return { ...def, ...JSON.parse(localStorage.getItem(PREFS_KEY) || '{}') }; } catch { return def; }
}

let state: State = {
  phase: 'loading', authConfigured: true, user: '', totp: false, recoveryLeft: 0, snap: null, online: true, lastOk: 0,
  page: 'home', moreOpen: false, drawer: null, modal: null, toasts: [], refreshing: false, prefs: loadPrefs(),
  svcTab: 'containers', svcFilter: 'all', search: '', imgFilter: 'all', imgSearch: '', netFilter: 'all',
  setTab: 'ui', range: '24h', hist: {}, busy: {}, checkingPc: false, scanning: false, wiz: null, pull: null, logs: null, catalog: null,
  updJobs: {}, updView: {}, busyBackup: false, toolDraft: null, editingTool: false,
};
const subs = new Set<() => void>();

export const get = () => state;
export function set(p: Partial<State> | ((s: State) => Partial<State>)) {
  state = { ...state, ...(typeof p === 'function' ? p(state) : p) };
  subs.forEach((f) => f());
}
export function useStore() {
  return useSyncExternalStore((f) => { subs.add(f); return () => subs.delete(f); }, get);
}

let api: Adapter;
export const adapter = () => api;

/* ---------- Avisos ---------- */
export function toast(t: Omit<Toast, 'id'>) {
  const id = Math.random().toString(36).slice(2);
  set((s) => ({ toasts: s.toasts.slice(-2).concat([{ ...t, id }]) }));
  setTimeout(() => dismiss(id), t.tone === 'err' ? 9000 : 6500);
}
export const dismiss = (id: string) => set((s) => ({ toasts: s.toasts.filter((t) => t.id !== id) }));

export function nav(page: Page, extra: Partial<State> = {}) {
  set({ page, drawer: null, moreOpen: false, ...extra });
  try { window.scrollTo(0, 0); } catch { /* sin ventana */ }
}

export function setPrefs(p: Partial<Prefs>) {
  set((s) => ({ prefs: { ...s.prefs, ...p } }));
  try { localStorage.setItem(PREFS_KEY, JSON.stringify(get().prefs)); } catch { /* almacenamiento bloqueado */ }
  if ('autoRefresh' in p) setupAuto();
}

/** Lleva al sitio del panel donde se configura lo que falta. */
export function needsSetup(title: string, note: string, page: Page, extra: Partial<State> = {}) {
  set({ modal: {
    title: 'Falta configurarlo',
    body: title + ': ' + note,
    confirmLabel: 'Configurar ahora', secondaryLabel: 'Ahora no',
    onSecondary: () => set({ modal: null }),
    onConfirm: () => { set({ modal: null }); nav(page, extra); },
  } });
}

export function confirmModal(m: Omit<ModalSpec, 'onConfirm'> & { onConfirm: () => void | Promise<void> }) {
  set({ modal: { ...m, onConfirm: () => { set({ modal: null }); void m.onConfirm(); } } });
}

/* ---------- Conexión con el servidor ---------- */
function handleErr(e: unknown, title: string): boolean {
  if (e instanceof ApiError && e.status === 401) {
    set({ phase: 'login', snap: null, user: '', modal: null, drawer: null, wiz: null, pull: null, logs: null });
    toast({ tone: 'warn', title: 'La sesión ha caducado', body: 'Vuelve a iniciar sesión.' });
    return true;
  }
  if (e instanceof ApiError && e.status === 0) set({ online: false });
  toast({ tone: 'err', title, body: e instanceof Error ? e.message : String(e) });
  return false;
}

export function accept(snap: Snapshot) { set({ snap, online: true, lastOk: Date.now() }); }

export async function load() {
  try {
    accept(await api.state());
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) { set({ phase: 'login' }); return; }
    set({ online: false });
  }
}

let autoTimer: ReturnType<typeof setInterval> | undefined;
export function setupAuto() {
  clearInterval(autoTimer);
  const ms = { off: 0, '30s': 30000, '1m': 60000, '5m': 300000 }[get().prefs.autoRefresh];
  if (ms) autoTimer = setInterval(() => { if (get().phase === 'app' && document.visibilityState === 'visible') void load(); }, ms);
}

export async function boot(a: Adapter) {
  api = a;
  try {
    const s = await api.session();
    set({ user: s.user, totp: !!s.totp, recoveryLeft: s.recoveryLeft || 0, phase: 'app' });
    await load();
    setupAuto();
  } catch (e) {
    let configured = true;
    try { configured = (await api.authStatus()).configured; } catch { /* sin servidor */ }
    set({ phase: 'login', authConfigured: configured, online: !(e instanceof ApiError && e.status === 0) });
  }
}

export async function login(user: string, password: string, remember: boolean, code?: string): Promise<string | 'needCode' | null> {
  try {
    const s = await api.login(user, password, remember, code);
    set({ user: s.user, phase: 'app', page: 'home', online: true });
    try { const ss = await api.session(); set({ totp: !!ss.totp, recoveryLeft: ss.recoveryLeft || 0 }); } catch { /* no es crítico */ }
    await load();
    setupAuto();
    return null;
  } catch (e) {
    if (e instanceof ApiError && e.data?.needCode) return 'needCode';
    return e instanceof Error ? e.message : 'No se pudo iniciar sesión.';
  }
}

export async function logout() {
  try { await api.logout(); } catch { /* da igual: se borra la sesión local */ }
  clearInterval(autoTimer);
  set({ phase: 'login', user: '', snap: null, modal: null, drawer: null, wiz: null, pull: null });
}

export async function refresh() {
  if (get().refreshing) return;
  set({ refreshing: true });
  try {
    accept(await api.refresh(get().page === 'network'));
  } catch (e) {
    if (!handleErr(e, 'No se pudo actualizar')) { /* se mantiene el último estado */ }
  } finally {
    set({ refreshing: false });
  }
}

/** Ejecuta una acción que devuelve el estado nuevo. */
export async function run(title: string, fn: () => Promise<Snapshot>, ok?: { title: string; body: string }) {
  try {
    accept(await fn());
    if (ok) toast({ tone: 'ok', ...ok });
    return true;
  } catch (e) {
    handleErr(e, title);
    return false;
  }
}

/** Sigue una tarea larga hasta que termine. */
export async function follow(id: string, onUpdate: (j: Job) => void): Promise<Job | null> {
  for (;;) {
    await new Promise((r) => setTimeout(r, 800));
    try {
      const j = await api.job(id);
      onUpdate(j);
      if (j.state !== 'running') { void load(); return j; }
    } catch (e) {
      if (handleErr(e, 'Se perdió el seguimiento de la tarea')) return null;
      if (e instanceof ApiError && e.status === 404) return null;
    }
  }
}

export async function loadLive(): Promise<LivePoint | null> {
  try {
    return await api.live();
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) handleErr(e, '');
    return null;
  }
}

export async function loadHistory(r: Range) {
  try {
    const h = await api.history(r);
    set((s) => ({ hist: { ...s.hist, [r]: h } }));
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) handleErr(e, '');
  }
}

export async function loadCatalog() {
  if (get().catalog) return;
  try { set({ catalog: await api.catalog() }); } catch (e) { handleErr(e, 'No se pudo cargar el catálogo'); }
}

export { handleErr };
