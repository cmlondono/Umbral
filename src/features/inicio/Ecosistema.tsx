import { useState } from 'react';
import { createPortal } from 'react-dom';
import type { NetDevice } from '../../data/types';
import type { Derived } from '../../state/derive';
import { adapter, nav, run, set } from '../../state/store';
import { Badge, Icon, Panel, agoIso, st, type IconName, type StateInfo } from '../../design/ui';
import { useDialog } from '../../design/useDialog';
import { deviceIcon } from '../red/Red';

interface Node { name: string; sub: string; icon: IconName; badge: StateInfo; go: () => void; dim?: boolean; style?: React.CSSProperties }

function TopoNode({ n }: { n: Node }) {
  return (
    <button className={'node' + (n.dim ? ' is-off' : '')} style={n.style} onClick={n.go}>
      <span className="ico-box"><Icon name={n.icon} /></span>
      <span style={{ minWidth: 0 }}><span className="node-t">{n.name}</span><span className="node-s">{n.sub}</span><Badge s={n.badge} /></span>
    </button>
  );
}

/* «Tu ecosistema»: Internet, router, servidor y Nextcloud arriba; debajo, los
 * equipos de casa que sigues (el PC y los que añadas), con su IP y su estado
 * actuales. Se añaden desde aquí, desde Red o desde Dispositivos. */
export function Ecosistema({ d }: { d: Derived }) {
  const x = d.s;
  const [adding, setAdding] = useState(false);
  const gw = x.network.devices.find((v) => v.ip === x.network.gateway);
  const pinned = x.network.devices.filter((v) => v.pinned && !v.registered && v.ip !== x.network.gateway);
  const devState = (v: NetDevice) => (!d.srvOn ? st('unknown') : v.online ? st('connected') : st('noReply', 'Desconectado'));
  const link = (on: boolean) => 'tl' + (on ? '' : ' dash');
  const pcConfigIP = x.config.pc.ip;
  const pcSub = !x.pc.configured ? 'Sin configurar' : x.pc.ip ? (pcConfigIP && pcConfigIP === x.pc.ip ? x.pc.ip : 'IP detectada: ' + x.pc.ip) : 'IP aún no detectada';
  const online = pinned.filter((v) => v.online).length + (d.pcState === 'on' ? 1 : 0);
  const total = pinned.length + (x.pc.configured ? 1 : 0);

  const top: Node[] = [
    { name: 'Internet', sub: 'Conexión del hogar', icon: 'globe', badge: !d.srvOn || x.internet == null ? st('unknown') : x.internet ? st('connected', 'Conectada') : st('offline', 'Sin conexión'), go: () => nav('network') },
    { name: 'Router', sub: x.router.connected && x.router.info?.model ? x.router.info.model : x.network.gateway || 'Puerta de enlace', icon: 'router', badge: !d.srvOn || !gw ? st('unknown') : gw.online ? st('appOk') : st('noReply'), go: () => set({ drawer: { type: 'device', id: 'router' } }) },
    { name: 'Servidor', sub: d.srvOn ? (x.docker.available ? d.running + ' de ' + d.ctrs.length + ' servicios activos' : 'Docker sin respuesta') : 'Último dato a las ' + d.lastKnown, icon: 'server', badge: st(d.srvOn ? 'online' : 'offline'), go: () => nav('server') },
  ];
  const nc: Node = { name: 'Nextcloud', sub: d.ncUrl.replace(/^https?:\/\//, '') || 'Sin dirección', icon: 'cloud', badge: d.ncState, go: () => nav('services'), dim: d.srvOn && !!d.nc && d.nc.state !== 'running' };

  return (
    <Panel title="Tu ecosistema" id="map-h" hint="Selecciona un elemento para ver sus detalles">
      <div className="topo">
        <TopoNode n={{ ...top[0], style: { gridColumn: 1, gridRow: 1 } }} />
        <div className={link(d.srvOn && x.internet === true)} style={{ gridColumn: 2, gridRow: 1 }} aria-hidden="true" />
        <TopoNode n={{ ...top[1], style: { gridColumn: 3, gridRow: 1 } }} />
        <div className={link(d.srvOn)} style={{ gridColumn: 4, gridRow: 1 }} aria-hidden="true" />
        <TopoNode n={{ ...top[2], style: { gridColumn: 5, gridRow: 1 } }} />
        {d.hasNC && <div className={link(d.ncUsable)} style={{ gridColumn: 6, gridRow: 1 }} aria-hidden="true" />}
        {d.hasNC && <TopoNode n={{ ...nc, style: { gridColumn: 7, gridRow: 1 } }} />}
        <div className="tl-v" style={{ gridColumn: 3, gridRow: 2 }} aria-hidden="true" />
      </div>

      <div className="lan">
        <div className="lan-h">
          <span className="strong small">En tu red</span>
          <span className="small muted">{total ? online + ' de ' + total + ' en línea' : 'Añade los equipos que quieras ver aquí'}</span>
        </div>
        <div className="lan-grid">
          <TopoNode n={{ name: x.pc.name || 'PC de escritorio', sub: pcSub, icon: 'monitor', badge: d.pcState === 'notConfigured' ? st('notConfigured') : st(d.pcState), go: () => set({ drawer: { type: 'device', id: 'pc' } }), dim: d.pcState !== 'on' }} />
          {pinned.map((v) => (
            <TopoNode key={v.mac} n={{ name: v.name || 'Sin nombre', sub: v.ip + (v.blocked ? ' · sin Internet' : ''), icon: deviceIcon(v, x.network.gateway), badge: devState(v), go: () => set({ drawer: { type: 'device', id: 'mac:' + v.mac } }), dim: !v.online }} />
          ))}
          <button className="node node-add" onClick={() => setAdding(true)} disabled={!d.srvOn} title={d.serverBlocked || undefined}>
            <span className="ico-box"><Icon name="plus" /></span>
            <span style={{ minWidth: 0 }}><span className="node-t">Añadir equipo</span><span className="node-s">Elige entre los detectados en la red</span></span>
          </button>
        </div>
      </div>
      {adding && <AddDevice d={d} onClose={() => setAdding(false)} />}
    </Panel>
  );
}

/* Lista de los equipos detectados que aún no están en el mapa. */
function AddDevice({ d, onClose }: { d: Derived; onClose: () => void }) {
  const ref = useDialog(true, onClose);
  const [q, setQ] = useState('');
  const [busy, setBusy] = useState('');
  const n = d.s.network;
  const cand = n.devices
    .filter((v) => !v.pinned && !v.registered && v.ip !== n.gateway)
    .filter((v) => { const t = q.trim().toLowerCase(); return !t || (v.name + ' ' + v.ip + ' ' + v.vendor + ' ' + v.mac).toLowerCase().includes(t); })
    .sort((a, b) => Number(b.online) - Number(a.online) || (a.name || '~').localeCompare(b.name || '~'));
  const add = async (v: NetDevice) => {
    setBusy(v.mac);
    await run('No se pudo añadir', () => adapter().updateDevice(v.mac, v.custom, true), { title: 'Añadido al mapa', body: (v.name || 'El equipo de ' + v.ip) + ' aparece ahora en Inicio y en Dispositivos. Púlsalo para ponerle nombre.' });
    setBusy('');
  };
  // Fuera del panel (su efecto de cristal atraparía la ventana) pero dentro de .app, donde viven los estilos.
  return createPortal(
    <>
      <div className="scrim" style={{ zIndex: 50 }} onClick={onClose} />
      <div className="modal-wrap">
        <div className="modal glass" role="dialog" aria-modal="true" aria-labelledby="ad-t" style={{ width: 'min(620px,100%)' }} ref={ref}>
          <h2 id="ad-t">Añadir un equipo al mapa</h2>
          <p>Son los equipos que el servidor ha visto en tu red. Si falta alguno, enciéndelo, conéctalo a la red y pulsa «Escanear ahora» en Red.</p>
          <input className="inp" style={{ marginTop: 12 }} placeholder="Buscar por nombre, IP o fabricante" value={q} onChange={(e) => setQ(e.target.value)} aria-label="Buscar equipo" />
          <div className="add-list">
            {cand.length ? cand.map((v) => (
              <div className="lrow" key={v.mac}>
                <span className="ico-box"><Icon name={deviceIcon(v, n.gateway)} /></span>
                <div className="grow" style={{ minWidth: 0 }}>
                  <span className="strong">{v.name || 'Sin nombre'}</span>
                  <span className="sub">{v.ip} · {v.vendor || v.mac}{v.online ? '' : ' · visto ' + agoIso(v.lastSeen).toLowerCase()}</span>
                </div>
                <Badge s={v.online ? st('connected') : st('noReply', 'Desconectado')} />
                <button className="btn btn-sm btn-primary" onClick={() => void add(v)} disabled={!!busy}>{busy === v.mac ? 'Añadiendo…' : 'Añadir'}</button>
              </div>
            )) : <p className="small muted" style={{ padding: '12px 0' }}>{q ? 'Ningún equipo coincide.' : 'Todos los equipos detectados ya están en el mapa.'}</p>}
          </div>
          <div className="modal-f"><button className="btn" onClick={() => { onClose(); nav('network'); }}>Ir a Red</button><button className="btn btn-primary" onClick={onClose}>Hecho</button></div>
        </div>
      </div>
    </>,
    document.querySelector('.app') || document.body,
  );
}
