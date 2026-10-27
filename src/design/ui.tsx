import type { ReactNode } from 'react';

/* Iconos de interfaz: los mismos trazos de la maqueta (24×24, línea de 1,7). */
export const ICONS = {
  home: 'M3 10.5 12 3l9 7.5V20a1 1 0 0 1-1 1h-5v-6H9v6H4a1 1 0 0 1-1-1z',
  devices: 'M3 5h13v10H3zM7 19h5M9.5 15v4M18 9h3v11h-3z',
  server: 'M4 4h16v6H4zM4 14h16v6H4zM8 7h.01M8 17h.01M12 7h4M12 17h4',
  boxes: 'M12 3l8 4.5v9L12 21l-8-4.5v-9zM12 12l8-4.5M12 12v9M12 12 4 7.5',
  network: 'M9 3h6v5H9zM12 8v4M5 12h14M5 12v4M19 12v4M12 12v4M3 16h4v5H3zM10 16h4v5h-4zM17 16h4v5h-4z',
  remote: 'M3 4h18v12H3zM8 20h8M12 16v4M9 11l3-3 3 3M12 8v5',
  workflow: 'M4 4h6v6H4zM14 14h6v6h-6zM7 10v3a4 4 0 0 0 4 4h3',
  settings: 'M4 6h9M17 6h3M4 12h3M11 12h9M4 18h11M19 18h1M15 4v4M9 10v4M17 16v4',
  gear: 'M12 19a7 7 0 1 0 0-14 7 7 0 0 0 0 14zM12 14.5a2.5 2.5 0 1 0 0-5 2.5 2.5 0 0 0 0 5zM12 2v3M12 19v3M2 12h3M19 12h3M4.9 4.9 7 7M17 17l2.1 2.1M4.9 19.1 7 17M17 7l2.1-2.1',
  check: 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zM8.5 12.5l2.5 2.5 4.5-5',
  alert: 'M12 3 2 20h20zM12 10v4M12 17h.01',
  x: 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zM9 9l6 6M15 9l-6 6',
  close: 'M6 6l12 12M18 6 6 18',
  info: 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zM12 11v5M12 8h.01',
  loader: 'M12 3a9 9 0 1 0 9 9',
  power: 'M12 3v8M6.3 6.6a8 8 0 1 0 11.4 0',
  external: 'M14 4h6v6M20 4l-9 9M18 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h5',
  wrench: 'M14.5 5.5a4 4 0 0 0 5 5L11 19a2.1 2.1 0 0 1-3-3zM5 19l2-2',
  chev: 'M9 6l6 6-6 6',
  copy: 'M9 9h11v11H9zM5 15H4V4h11v1',
  play: 'M7 4.5v15l12-7.5z',
  stop: 'M6 6h12v12H6z',
  restart: 'M4 12a8 8 0 1 0 2.3-5.7L4 8.5M4 4v4.5h4.5',
  refresh: 'M20 12a8 8 0 1 1-2.3-5.7L20 8.5M20 4v4.5h-4.5',
  radar: 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zM12 16a4 4 0 1 0 0-8 4 4 0 0 0 0 8zM12 12h.01',
  globe: 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zM3 12h18M12 3c2.5 2.5 3.5 5.5 3.5 9s-1 6.5-3.5 9c-2.5-2.5-3.5-5.5-3.5-9s1-6.5 3.5-9',
  router: 'M3 13h18v7H3zM7 16.5h.01M11 16.5h.01M8 13l-2-8M16 13l2-8',
  monitor: 'M3 4h18v12H3zM8 20h8M12 16v4',
  cloud: 'M7 18a5 5 0 1 1 .8-9.9A6 6 0 0 1 19 10a4 4 0 0 1-1 8z',
  phone: 'M7 2h10v20H7zM11 18h2',
  laptop: 'M4 5h16v11H4zM2 19h20',
  tv: 'M3 5h18v12H3zM8 21h8',
  terminal: 'M4 17l6-5-6-5M12 19h8',
  activity: 'M3 12h4l3-8 4 16 3-8h4',
  lock: 'M5 11h14v10H5zM8 11V7a4 4 0 0 1 8 0v4',
  user: 'M12 12a4 4 0 1 0 0-8 4 4 0 0 0 0 8zM4 21a8 8 0 0 1 16 0',
  shield: 'M12 3l8 3v6c0 5-3.5 8-8 9-4.5-1-8-4-8-9V6z',
  ban: 'M12 3l8 3v6c0 5-3.5 8-8 9-4.5-1-8-4-8-9V6zM8.5 8.5l7 7',
  clock: 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zM12 7v5l3 2',
  nfc: 'M6 8.5a5 5 0 0 1 0 7M9.5 6a9 9 0 0 1 0 12M13 4a13 13 0 0 1 0 16M16.5 2.5a17 17 0 0 1 0 19',
  mic: 'M9 3h6v10a3 3 0 0 1-6 0zM5 11a7 7 0 0 0 14 0M12 18v3',
  bell: 'M6 16V11a6 6 0 0 1 12 0v5l2 2H4zM10 21h4',
  cpu: 'M7 7h10v10H7zM10 10h4v4h-4zM9 3v4M15 3v4M9 17v4M15 17v4M3 9h4M3 15h4M17 9h4M17 15h4',
  memory: 'M3 7h18v10H3zM7 11v2M11 11v2M15 11v2M6 17v3M18 17v3',
  disk: 'M4 13l2.5-8h11L20 13v6H4zM4 13h16M8 16h.01',
  updown: 'M8 4v16M4 8l4-4 4 4M16 20V4M12 16l4 4 4-4',
  circle: 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18z',
  dot: 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18z',
  plus: 'M12 5v14M5 12h14',
  trash: 'M4 7h16M10 11v6M14 11v6M6 7l1 13h10l1-13M9 7V4h6v3',
  download: 'M12 4v11M7.5 10.5 12 15l4.5-4.5M5 20h14',
  search: 'M11 18a7 7 0 1 0 0-14 7 7 0 0 0 0 14zM20 20l-4-4',
  more: 'M5 12h.01M12 12h.01M19 12h.01',
  help: 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18zM9.6 9.4a2.5 2.5 0 1 1 3.4 2.4c-.6.3-1 .9-1 1.6v.4M12 17h.01',
  eye: 'M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12zM12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6z',
  eyeOff: 'M3 3l18 18M10.6 5.1A10 10 0 0 1 12 5c6.5 0 10 7 10 7a17 17 0 0 1-3 3.9M6.6 6.6A17 17 0 0 0 2 12s3.5 7 10 7a9.8 9.8 0 0 0 5.4-1.6M9.9 9.9a3 3 0 0 0 4.2 4.2',
  logout: 'M15 4h4a1 1 0 0 1 1 1v14a1 1 0 0 1-1 1h-4M10 16l-4-4 4-4M6 12h11',
} as const;
export type IconName = keyof typeof ICONS;

/* Glifos de los símbolos de estado (círculo relleno + glifo blanco). */
const GLYPHS = {
  check: 'M7.6 12.4l2.9 2.9 5.9-6.1', x: 'M9 9l6 6M15 9l-6 6', minus: 'M8 12h8',
  help: 'M9.8 9.7a2.3 2.3 0 1 1 3 2.2c-.6.2-.8.7-.8 1.3v.3M12 16.4h.01', loader: 'M12 7a5 5 0 1 0 5 5',
  alert: 'M12 7.2v5.6M12 16.4h.01', dash: 'M8.5 12h.01M12 12h.01M15.5 12h.01', wrench: 'M8.5 12h.01M12 12h.01M15.5 12h.01',
  radar: 'M12 15a3 3 0 1 0 0-6 3 3 0 0 0 0 6z', up: 'M12 16.5V8M8.5 11.5 12 8l3.5 3.5',
} as const;
export type Glyph = keyof typeof GLYPHS;

export function Icon({ name, className = 'i', style }: { name: IconName; className?: string; style?: React.CSSProperties }) {
  return <svg className={className} viewBox="0 0 24 24" aria-hidden="true" style={style}><path d={ICONS[name]} /></svg>;
}

export function Sym({ glyph, className = 'i sym', spin }: { glyph: Glyph; className?: string; spin?: boolean }) {
  return (
    <svg className={className + (spin ? ' spin' : '')} viewBox="0 0 24 24" aria-hidden="true">
      <circle cx="12" cy="12" r="10" /><path d={GLYPHS[glyph]} />
    </svg>
  );
}

export function glyphPath(g: Glyph) { return GLYPHS[g]; }

/* ---------- Estados ---------- */
type Tone = 'ok' | 'warn' | 'err' | 'off' | 'busy' | 'setup';
const STATES = {
  on: ['Encendido', 'ok', 'check'], off: ['Apagado', 'off', 'minus'], unknown: ['Desconocido', 'off', 'help'], waking: ['Encendiendo…', 'busy', 'loader'],
  offline: ['Sin respuesta', 'err', 'x'], online: ['En línea', 'ok', 'check'],
  running: ['En ejecución', 'ok', 'check'], stopped: ['Detenido', 'off', 'minus'], restarting: ['Reiniciando…', 'busy', 'loader'], stopping: ['Deteniendo…', 'busy', 'loader'], starting: ['Iniciando…', 'busy', 'loader'],
  removing: ['Eliminando…', 'busy', 'loader'], paused: ['En pausa', 'off', 'minus'], created: ['Creado, sin iniciar', 'off', 'minus'], dead: ['Con error', 'err', 'x'],
  appOk: ['Responde', 'ok', 'check'], appFail: ['No responde', 'err', 'x'], healthy: ['Saludable', 'ok', 'check'], unhealthy: ['No saludable', 'err', 'x'], healthStarting: ['Arrancando…', 'busy', 'loader'],
  noCheck: ['Sin comprobación', 'off', 'dash'], na: ['No aplica', 'off', 'dash'],
  connected: ['Conectado', 'ok', 'check'], noReply: ['Sin respuesta', 'off', 'minus'],
  pending: ['Sin configurar', 'setup', 'wrench'], notConfigured: ['Sin configurar', 'setup', 'wrench'],
  active: ['Activa', 'ok', 'check'], disabled: ['Desactivada', 'off', 'minus'],
  resOk: ['Correcto', 'ok', 'check'], resErr: ['Error', 'err', 'x'],
  warn: ['Aviso', 'warn', 'alert'], update: ['Disponible', 'busy', 'up'], err: ['Problema', 'err', 'x'], ok: ['Correcto', 'ok', 'check'], detected: ['Detectado', 'off', 'radar'],
} as const satisfies Record<string, readonly [string, Tone, Glyph]>;
export type StateKey = keyof typeof STATES;

export interface StateInfo { label: string; tone: Tone; glyph: Glyph; busy: boolean }

export function st(k: StateKey, label?: string): StateInfo {
  const m = STATES[k];
  return { label: label || m[0], tone: m[1], glyph: m[2], busy: m[2] === 'loader' };
}

export function Badge({ s, className = '', iconOnly }: { s: StateInfo; className?: string; iconOnly?: boolean }) {
  return (
    <span className={`badge b-${s.tone} ${className}`}>
      <Sym glyph={s.glyph} spin={s.busy} />{iconOnly ? <span className="sr">{s.label}</span> : s.label}
    </span>
  );
}

/* ---------- Acciones ----------
 * Cada botón es de un tipo: disponible, requiere un servicio externo o
 * pendiente de configuración. Si está bloqueado, dice por qué. */
export type ActKind = 'primary' | 'normal' | 'setup' | 'external' | 'danger';

export interface Action {
  label: string;
  icon: IconName;
  kind: ActKind;
  blocked: string;
  note: string;
  spin?: boolean;
  run: () => void;
  tag: string;
}

export function act(o: { label: string; icon?: IconName; kind?: ActKind; blocked?: string; note?: string; spin?: boolean; run: () => void }): Action {
  const kind = o.kind || 'normal';
  const icon = o.icon || (kind === 'setup' ? 'wrench' : kind === 'external' ? 'external' : 'chev');
  return { label: o.label, icon, kind, blocked: o.blocked || '', note: o.blocked || o.note || '', spin: o.spin, run: o.run,
    tag: kind === 'setup' ? 'Falta configurar' : kind === 'external' ? 'Servicio externo' : '' };
}

export function btnCls(kind: ActKind) {
  return 'btn' + (kind === 'primary' ? ' btn-primary' : kind === 'danger' ? ' btn-danger' : kind === 'setup' ? ' btn-setup' : '');
}

export function ActButton({ a, small = true, showTitle = true }: { a: Action; small?: boolean; showTitle?: boolean }) {
  return (
    <button className={btnCls(a.kind) + (small ? ' btn-sm' : '')} onClick={a.blocked ? undefined : a.run} disabled={!!a.blocked} title={showTitle ? a.note || undefined : undefined}>
      <Icon name={a.spin ? 'loader' : a.icon} className={a.spin ? 'i spin' : 'i'} />{a.label}
    </button>
  );
}

/* ---------- Controles ---------- */
export function Seg<T extends string>({ value, options, onChange, label, counts, role = 'group' }: {
  value: T; options: [T, string][]; onChange: (v: T) => void; label: string; counts?: Partial<Record<T, number>>; role?: 'group' | 'tablist';
}) {
  return (
    <div className="seg" role={role} aria-label={label}>
      {options.map(([k, l]) => (
        <button key={k} className={value === k ? 'on' : ''} {...(role === 'tablist' ? { role: 'tab', 'aria-selected': value === k } : { 'aria-pressed': value === k })} onClick={() => onChange(k)}>
          {l}{counts && counts[k] !== undefined && <span className="cnt">{counts[k]}</span>}
        </button>
      ))}
    </div>
  );
}

export function Switch({ on, onChange, label, disabled }: { on: boolean; onChange: (v: boolean) => void; label: string; disabled?: boolean }) {
  return <button className={'sw' + (on ? ' on' : '')} role="switch" aria-checked={on} aria-label={label} disabled={disabled} onClick={() => onChange(!on)} />;
}

export function Search({ value, onChange, placeholder, label }: { value: string; onChange: (v: string) => void; placeholder: string; label: string }) {
  return (
    <label className="search">
      <Icon name="search" className="i i-sm" /><span className="sr">{label}</span>
      <input type="search" placeholder={placeholder} value={value} onChange={(e) => onChange(e.target.value)} />
    </label>
  );
}

export function Panel({ title, id, hint, right, children, className = 'panel' }: { title?: ReactNode; id?: string; hint?: ReactNode; right?: ReactNode; children: ReactNode; className?: string }) {
  return (
    <section className={className} aria-labelledby={id}>
      {title !== undefined && (
        <div className="ph">
          {hint !== undefined && typeof hint !== 'string' ? <div><h2 id={id}>{title}</h2>{hint}</div> : <h2 id={id}>{title}</h2>}
          {typeof hint === 'string' && <span className="hint">{hint}</span>}
          {right}
        </div>
      )}
      {children}
    </section>
  );
}

export function LinkBtn({ children, onClick }: { children: ReactNode; onClick: () => void }) {
  return <button className="link-btn" onClick={onClick}>{children}<Icon name="chev" className="i i-sm" /></button>;
}

export function Banner({ tone, icon, title, children, style }: { tone: 'err' | 'warn' | 'setup' | 'ok'; icon: IconName; title?: ReactNode; children?: ReactNode; style?: React.CSSProperties }) {
  return (
    <div className={'banner ' + tone} role="status" style={style}>
      <Icon name={icon} />
      <div style={{ flex: 1, minWidth: 0 }}>{title && <b>{title}</b>}{children && <span>{children}</span>}</div>
    </div>
  );
}

export function Empty({ icon, children, style }: { icon: IconName; children: ReactNode; style?: React.CSSProperties }) {
  return <div className="empty" style={style}><Icon name={icon} /><span>{children}</span></div>;
}

/* ---------- Formato ---------- */
export const fmt = (n: number, d = 1) => n.toFixed(d).replace('.', ',');
export const hhmm = (t: number | string | Date) => { const d = new Date(t); return d.getHours() + ':' + String(d.getMinutes()).padStart(2, '0'); };
export function ago(m: number) {
  if (m < 1) return 'Ahora';
  if (m < 60) return 'Hace ' + Math.round(m) + ' min';
  if (m < 1440) return 'Hace ' + Math.floor(m / 60) + ' h';
  const d = Math.floor(m / 1440); return 'Hace ' + d + (d === 1 ? ' día' : ' días');
}
export const agoIso = (iso: string | null | undefined) => (iso ? ago(Math.max(0, (Date.now() - new Date(iso).getTime()) / 60000)) : '—');
export function dur(sec: number) { const m = sec / 60; const d = Math.floor(m / 1440), h = Math.floor((m % 1440) / 60); return d ? d + ' d ' + h + ' h' : h + ' h ' + Math.round(m % 60) + ' min'; }
export const memLabel = (mb: number) => (mb >= 1000 ? fmt(mb / 1024) + ' GB' : Math.round(mb) + ' MB');
export const gib = (mb: number) => fmt(mb / 1024) + ' GiB';
