import { useEffect, useState } from 'react';
import { derive, type Derived } from '../state/derive';
import { logout, nav, refresh, set, useStore, type Page, type State } from '../state/store';
import { Icon, hhmm, type IconName } from '../design/ui';
import { Layers } from './Layers';
import { Login } from './Login';
import { Inicio } from '../features/inicio/Inicio';
import { Dispositivos } from '../features/dispositivos/Dispositivos';
import { Servidor } from '../features/servidor/Servidor';
import { Servicios } from '../features/servicios/Servicios';
import { Red } from '../features/red/Red';
import { AccesoRemoto } from '../features/acceso-remoto/AccesoRemoto';
import { Automatizaciones } from '../features/automatizaciones/Automatizaciones';
import { Ajustes } from '../features/ajustes/Ajustes';

const TITLES: Record<Page, [string, string]> = {
  home: ['Inicio', 'Estado general de tus equipos y servicios.'],
  devices: ['Dispositivos', 'Equipos de tu ecosistema y lo que puedes hacer con cada uno.'],
  server: ['Servidor', 'Recursos y mantenimiento del servidor.'],
  services: ['Servicios', 'Contenedores Docker del servidor y el estado real de cada aplicación.'],
  network: ['Red', 'Dispositivos detectados en tu red doméstica.'],
  remote: ['Acceso remoto', 'Conéctate a tus equipos desde casa o a través de Tailscale.'],
  auto: ['Automatizaciones', 'Tareas que el servidor ejecuta solo, como encender el PC a una hora.'],
  settings: ['Ajustes', 'Preferencias del panel, dispositivos, integraciones y seguridad.'],
};

const NAV: Record<Page, [string, IconName]> = {
  home: ['Inicio', 'home'], devices: ['Dispositivos', 'devices'], server: ['Servidor', 'server'], services: ['Servicios', 'boxes'],
  network: ['Red', 'network'], remote: ['Acceso remoto', 'remote'], auto: ['Automatizaciones', 'workflow'], settings: ['Ajustes', 'settings'],
};
const MORE_DESC: Partial<Record<Page, string>> = { server: 'Recursos, discos y herramientas', remote: 'Escritorio remoto y Tailscale', auto: 'Encendido programado del PC', settings: 'Preferencias e integraciones' };

function useSystemDark() {
  const [q] = useState(() => window.matchMedia('(prefers-color-scheme: dark)'));
  const [dark, setDark] = useState(q.matches);
  useEffect(() => {
    const f = () => setDark(q.matches);
    q.addEventListener('change', f);
    return () => q.removeEventListener('change', f);
  }, [q]);
  return dark;
}

export function rootClass(s: State, sysDark: boolean) {
  const p = s.prefs;
  const dark = p.theme === 'dark' || (p.theme === 'auto' && sysDark);
  return 'app' + (dark ? ' dark' : '') + (p.density === 'compact' ? ' compact' : '') + (p.wallpaper === 'plain' ? ' plain' : '') + (p.transparency === 'reduced' ? ' solid' : '');
}

export function App() {
  const s = useStore();
  const sysDark = useSystemDark();
  const cls = rootClass(s, sysDark);
  useEffect(() => {
    document.body.style.background = cls.includes('dark') ? '#000' : '#e6e9ee';
    document.documentElement.style.colorScheme = cls.includes('dark') ? 'dark' : 'light';
  }, [cls]);

  if (s.phase === 'loading') return <div className={cls} style={{ minHeight: '100vh' }} aria-busy="true" />;
  if (s.phase === 'login') return <div className={cls}><Login /><Layers d={null} /></div>;
  const d = derive(s);
  return (
    <div className={cls}>
      <Shell s={s} d={d} />
      <Layers d={d} />
    </div>
  );
}

function flags(d: Derived | null): Partial<Record<Page, [string, string]>> {
  if (!d) return {};
  const f: Partial<Record<Page, [string, string]>> = {};
  if (!d.srvOn) f.server = ['err', 'Sin respuesta'];
  else if (d.diskMax && d.diskMax.pct >= 80) f.server = [d.diskMax.pct >= 90 ? 'err' : 'warn', 'Aviso de almacenamiento'];
  const svcIssue = d.ctrs.some((c) => c.issue) || (d.srvOn && !d.s.docker.available);
  if (svcIssue) f.services = [d.srvOn ? 'err' : 'off', d.srvOn ? 'Un servicio requiere atención' : 'Estado desconocido'];
  if (d.srvOn && !d.s.tailscale.running) f.remote = ['warn', 'Tailscale no está conectado'];
  return f;
}

function Shell({ s, d }: { s: State; d: Derived | null }) {
  const fl = flags(d);
  const tabs: Page[] = ['home', 'devices', 'server', 'services', 'network', 'remote', 'auto'];
  const moreKeys: Page[] = ['server', 'remote', 'auto', 'settings'];
  const inMore = moreKeys.includes(s.page);
  const moreFlag = moreKeys.map((k) => fl[k]).find(Boolean);
  const Flag = ({ f }: { f?: [string, string] }) => (f ? <><span className={'dot dot-' + f[0]} title={f[1]} /><span className="sr">{f[1]}</span></> : null);
  const [title, desc] = TITLES[s.page];

  return (
    <>
      <header className="topbar">
        <div className="tb-inner">
          <div className="brand cap glass">
            <svg width="28" height="28" viewBox="0 0 32 32" aria-hidden="true"><rect x="1" y="1" width="30" height="30" rx="7.5" fill="#1d1d1f" /><path d="M10 23V13.5a6 6 0 0 1 12 0V23" fill="none" stroke="#ffffff" strokeWidth="2.6" strokeLinecap="round" /><circle cx="16" cy="21.5" r="2.3" fill="#30d158" /></svg>
            <span className="brand-name">Umbral</span>
          </div>
          <nav className="pill glass" aria-label="Secciones">
            {tabs.map((k) => (
              <button key={k} className={'pill-btn' + (s.page === k ? ' on' : '')} aria-current={s.page === k ? 'page' : undefined} onClick={() => nav(k)}>
                {NAV[k][0]}<Flag f={fl[k]} />
              </button>
            ))}
          </nav>
          <div className="tb-right cap glass">
            <button className="demo-cap" onClick={() => openSession(s.user)} aria-label={'Sesión iniciada como ' + s.user}>
              <Icon name="user" className="i i-sm" /><span>{s.user}</span>
            </button>
            <button className={'round' + (s.page === 'settings' ? ' on' : '')} aria-label="Ajustes" aria-current={s.page === 'settings' ? 'page' : undefined} onClick={() => nav('settings')} title="Ajustes">
              <Icon name="gear" />
            </button>
          </div>
        </div>
      </header>

      <nav className="mtabs glass" aria-label="Secciones">
        {(['home', 'devices', 'services', 'network'] as Page[]).map((k) => (
          <button key={k} className={'mtab' + (s.page === k ? ' on' : '')} aria-current={s.page === k ? 'page' : undefined} onClick={() => nav(k)}>
            <span className="mt-ic"><Icon name={NAV[k][1]} /><Flag f={fl[k]} /></span><span>{NAV[k][0]}</span>
          </button>
        ))}
        <button className={'mtab' + (inMore ? ' on' : '')} aria-current={inMore ? 'page' : undefined} onClick={() => set({ moreOpen: true })}>
          <span className="mt-ic"><Icon name="more" /><Flag f={moreFlag} /></span><span>Más</span>
        </button>
      </nav>

      <main className="main">
        <div className={'wrap' + (s.refreshing ? ' busy' : '')} aria-busy={s.refreshing}>
          <header className="phead">
            <div><h1>{title}</h1><p>{desc}</p></div>
            <div className="pmeta">
              <span>{d ? (d.srvOn ? 'Actualizado a las ' + hhmm(s.lastOk) : 'Último dato recibido a las ' + d.lastKnown) : 'Cargando…'}</span>
              <button className="btn btn-sm" onClick={() => void refresh()} disabled={s.refreshing}>
                <Icon name="refresh" className={s.refreshing ? 'i spin' : 'i'} />{s.refreshing ? 'Actualizando…' : 'Actualizar'}
              </button>
            </div>
          </header>

          {d && !d.srvOn && s.page !== 'home' && s.page !== 'settings' && (
            <div className="banner err" role="status">
              <Icon name="x" />
              <div><b>El servidor no envía datos desde las {d.lastKnown}</b><span>Lo que ves es el último estado conocido. Las acciones que dependen del servidor están en pausa hasta que vuelva a responder.</span></div>
            </div>
          )}

          {!d ? <div className="panel"><div className="empty"><Icon name="loader" className="i spin" />Cargando el estado del servidor…</div></div> : (
            <>
              {s.page === 'home' && <Inicio d={d} />}
              {s.page === 'devices' && <Dispositivos d={d} />}
              {s.page === 'server' && <Servidor d={d} />}
              {s.page === 'services' && <Servicios d={d} />}
              {s.page === 'network' && <Red d={d} />}
              {s.page === 'remote' && <AccesoRemoto d={d} />}
              {s.page === 'auto' && <Automatizaciones d={d} />}
              {s.page === 'settings' && <Ajustes d={d} />}
            </>
          )}
        </div>
      </main>

      {s.moreOpen && (
        <>
          <div className="scrim" onClick={() => set({ moreOpen: false })} />
          <div className="sheet glass" role="dialog" aria-modal="true" aria-labelledby="more-h">
            <div className="sheet-grab" aria-hidden="true" />
            <h2 id="more-h">Más</h2>
            {moreKeys.map((k) => (
              <button key={k} className="sheet-row" aria-current={s.page === k ? 'page' : undefined} onClick={() => nav(k)}>
                <span className="ico-box"><Icon name={NAV[k][1]} /></span>
                <span className="grow">{NAV[k][0]}<span className="sub">{MORE_DESC[k]}</span></span>
                <Flag f={fl[k]} />
                <Icon name="chev" className="i i-sm" style={{ color: 'var(--ink3)' }} />
              </button>
            ))}
          </div>
        </>
      )}
    </>
  );
}

function openSession(user: string) {
  set({ modal: {
    title: 'Sesión iniciada como ' + user,
    body: 'Tu sesión se guarda en el servidor. Si cierras sesión, tendrás que volver a escribir la contraseña en este dispositivo.',
    confirmLabel: 'Seguir', noCancel: true, secondaryLabel: 'Cerrar sesión',
    onSecondary: () => void logout(),
    onConfirm: () => set({ modal: null }),
  } });
}
