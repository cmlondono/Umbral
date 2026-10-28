import { useState } from 'react';
import type { NetDevice } from '../../data/types';
import type { Derived } from '../../state/derive';
import { adapter, confirmModal, run, set, useStore } from '../../state/store';
import { Badge, Icon, Panel, Seg, agoIso, st, type IconName } from '../../design/ui';
import { usePaged } from '../../design/Pager';

export function deviceIcon(v: NetDevice, gateway: string): IconName {
  if (v.registered === 'server') return 'server';
  if (v.registered === 'pc') return 'monitor';
  if (v.ip === gateway) return 'router';
  const t = (v.name + ' ' + v.vendor).toLowerCase();
  if (/iphone|android|phone|galaxy|pixel|xiaomi|redmi|huawei|motorola/.test(t)) return 'phone';
  if (/tv|webos|roku|chromecast|fire|bravia|tizen/.test(t)) return 'tv';
  if (/macbook|laptop|portatil|notebook|desktop|-pc|windows/.test(t)) return 'laptop';
  return v.name ? 'devices' : 'help';
}

export function deviceSub(v: NetDevice, gateway: string) {
  if (v.registered) return 'Registrado';
  if (v.ip === gateway) return 'Puerta de enlace';
  return v.vendor || (v.name ? 'Nombre anunciado por el equipo' : 'Sin nombre anunciado');
}

export function Red({ d }: { d: Derived }) {
  const s = useStore();
  const [edit, setEdit] = useState<{ mac: string; name: string; pinned: boolean } | null>(null);
  const n = d.s.network, ts = d.s.tailscale;
  const devs = n.devices.map((v) => ({ ...v, status: !d.srvOn ? 'unknown' : v.online ? 'connected' : 'noReply' } as const));
  const cnt = { all: devs.length, connected: devs.filter((v) => v.status === 'connected').length, noReply: devs.filter((v) => v.status !== 'connected').length };
  const rows = devs.filter((v) => s.netFilter === 'all' || (s.netFilter === 'connected' ? v.status === 'connected' : v.status !== 'connected'));
  const pg = usePaged(rows, 'red');
  const tsOnline = ts.peers.filter((p) => p.online).length;
  const rt = d.s.router;
  const rtOn = d.srvOn && rt.connected;
  const isBlocked = (v: NetDevice) => !!v.blocked || rt.blocked.includes(v.mac);
  const block = (v: NetDevice) => confirmModal({
    title: '¿Quitar Internet a ' + (v.name || v.mac) + '?', body: 'El router dejará de dar salida a Internet a este equipo (por su MAC). Seguirá conectado a la red de casa. Las conexiones ya abiertas pueden tardar unos minutos en cortarse. Si usa una MAC privada que cambia, la regla dejará de aplicarse.',
    confirmLabel: 'Quitar Internet', confirmCls: 'btn btn-danger-solid',
    onConfirm: () => void run('No se pudo bloquear', () => adapter().routerOp('block', v.mac), { title: 'Sin Internet', body: (v.name || v.mac) + ' ya no sale a Internet.' }),
  });
  const unblock = (v: NetDevice) => void run('No se pudo desbloquear', () => adapter().routerOp('unblock', v.mac), { title: 'Internet devuelto', body: (v.name || v.mac) + ' vuelve a tener Internet.' });
  const reboot = () => confirmModal({
    title: '¿Reiniciar el router?', body: 'Toda la casa se quedará sin red e Internet uno o dos minutos, también este panel y Nextcloud para quien esté fuera. Tailscale volverá a conectar solo.',
    confirmLabel: 'Reiniciar router', confirmCls: 'btn btn-danger-solid',
    onConfirm: () => void run('No se pudo reiniciar el router', () => adapter().routerOp('reboot'), { title: 'Reiniciando el router', body: 'Vuelve en uno o dos minutos.' }),
  });
  const scan = async () => {
    set({ scanning: true });
    await run('No se pudo escanear la red', () => adapter().scanNetwork(), { title: 'Red escaneada', body: 'La lista muestra lo que respondió ahora mismo.' });
    set({ scanning: false });
  };
  const save = async () => {
    if (!edit) return;
    const name = edit.name.trim();
    if (await run('No se pudo guardar', () => adapter().updateDevice(edit.mac, name, edit.pinned), { title: 'Guardado', body: (name || 'El dispositivo') + (edit.pinned ? ' aparece ahora en Dispositivos.' : ' ya no aparece en Dispositivos.') })) setEdit(null);
  };
  const strip = [
    { k: 'Internet', badge: !d.srvOn || d.s.internet == null ? st('unknown') : d.s.internet ? st('connected', 'Conectada') : st('offline', 'Sin conexión'), sub: d.srvOn ? 'Comprobado desde el servidor' : 'Sin datos del servidor' },
    { k: 'Router', v: rtOn && rt.info?.model ? rt.info.model : n.gateway || '—', sub: rtOn && rt.info ? rt.info.firmware + (rt.blocked.length ? ' · ' + rt.blocked.length + ' sin Internet' : '') : 'Puerta de enlace' },
    { k: 'Red local', v: n.subnet || '—', sub: devs.length + ' dispositivos detectados' },
    { k: 'Tailscale', v: ts.running ? 'Conectado' : ts.installed ? 'Desconectado' : 'No instalado', sub: ts.running ? tsOnline + ' de ' + ts.peers.length + ' dispositivos en línea' : 'Acceso desde fuera de casa' },
    { k: 'Último escaneo', v: n.lastScan ? agoIso(n.lastScan) : 'Aún no', sub: 'Cada ' + d.s.config.scanMinutes + ' min desde el servidor' },
  ];

  return (
    <>
      <section className="panel">
        <dl className="strip" style={{ margin: 0 }}>
          {strip.map((f) => <div key={f.k}><dt>{f.k}</dt><dd>{f.badge ? <Badge s={f.badge} /> : f.v}<span className="sub">{f.sub}</span></dd></div>)}
        </dl>
      </section>

      <Panel title="Dispositivos detectados" id="net-h" hint="Todo lo que se conecta a tu red aparece aquí solo">
        <div className="toolbar">
          <Seg label="Filtrar dispositivos" value={s.netFilter} onChange={(v) => set({ netFilter: v })} counts={cnt} options={[['all', 'Todos'], ['connected', 'Conectados'], ['noReply', 'Sin respuesta']]} />
          <button className="btn btn-sm" onClick={() => void scan()} disabled={!d.srvOn || s.scanning}><Icon name={s.scanning ? 'loader' : 'radar'} className={s.scanning ? 'i spin' : 'i'} />{s.scanning ? 'Escaneando…' : 'Escanear ahora'}</button>
        </div>
        {rows.length ? (
          <div className="tbox">
            <table className="tbl">
              <thead><tr><th>Nombre</th><th>Dirección IP</th><th className="hide-sm">Dirección MAC</th><th className="hide-sm">Última detección</th><th>Estado</th><th><span className="sr">Acciones</span></th></tr></thead>
              <tbody>
                {pg.rows.map((v) => {
                  const isNew = Date.now() - new Date(v.firstSeen).getTime() < 24 * 3600e3 && !!n.lastScan && new Date(n.lastScan).getTime() - new Date(v.firstSeen).getTime() > 60e3;
                  const ed = edit?.mac === v.mac ? edit : null;
                  return (
                    <tr key={v.mac}>
                      <td>
                        <span style={{ display: 'flex', alignItems: 'flex-start', gap: 10 }}>
                          <Icon name={deviceIcon(v, n.gateway)} style={{ color: 'var(--ink3)', marginTop: 2 }} />
                          <span style={{ minWidth: 0 }}>
                            <span className="strong">{v.name || 'Dispositivo sin nombre'}</span>
                            {isNew && <> <span className="tag solid">Nuevo</span></>}
                            {v.pinned && <> <span className="tag">En Dispositivos</span></>}
                            <span className="sub">{deviceSub(v, n.gateway)}<span className="show-sm"> · {v.mac}</span></span>
                            {ed && (
                              <span className="rename">
                                <input className="inp" aria-label={'Nombre de ' + v.mac} placeholder={v.name || 'Por ejemplo, Televisor de la sala'} value={ed.name} maxLength={40} autoFocus
                                  onChange={(e) => setEdit({ ...ed, name: e.target.value })} onKeyDown={(e) => { if (e.key === 'Enter') void save(); if (e.key === 'Escape') setEdit(null); }} />
                                <label className="small" style={{ display: 'flex', gap: 6, alignItems: 'center', cursor: 'pointer' }}>
                                  <input type="checkbox" checked={ed.pinned} onChange={(e) => setEdit({ ...ed, pinned: e.target.checked })} style={{ accentColor: 'var(--accent)' }} />Mostrar en Dispositivos
                                </label>
                                <button className="btn btn-sm btn-primary" onClick={() => void save()}>Guardar</button>
                                <button className="btn btn-sm btn-quiet" onClick={() => setEdit(null)}>Cancelar</button>
                              </span>
                            )}
                          </span>
                        </span>
                      </td>
                      <td className="nowrap">{v.ip}</td>
                      <td className="hide-sm nowrap small">{v.mac}</td>
                      <td className="hide-sm nowrap">{v.online ? 'Ahora' : agoIso(v.lastSeen)}</td>
                      <td><Badge s={st(v.status)} />{isBlocked(v) && <span style={{ display: 'block', marginTop: 4 }}><Badge s={st('warn', 'Sin Internet')} /></span>}</td>
                      <td className="num"><div className="row-actions">
                        {!ed && rtOn && v.ip === n.gateway && <button className="btn btn-sm btn-quiet btn-danger" onClick={reboot}><Icon name="restart" />Reiniciar router</button>}
                        {!ed && rtOn && v.registered !== 'server' && v.ip !== n.gateway && (isBlocked(v)
                          ? <button className="btn btn-sm" onClick={() => unblock(v)}>Devolver Internet</button>
                          : <button className="btn btn-sm btn-quiet btn-danger" onClick={() => block(v)} title="El router deja de darle salida a Internet">Quitar Internet</button>)}
                        {!ed && !v.registered && v.ip !== n.gateway && (
                          v.pinned
                            ? <button className="btn btn-sm btn-quiet" onClick={() => setEdit({ mac: v.mac, name: v.custom, pinned: true })} disabled={!d.srvOn}>Editar</button>
                            : <button className="btn btn-sm" onClick={() => setEdit({ mac: v.mac, name: v.custom, pinned: true })} disabled={!d.srvOn}><Icon name="plus" />Añadir a Dispositivos</button>
                        )}
                      </div></td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        ) : (
          <div className="empty"><Icon name="radar" />{n.lastScan ? 'Ningún dispositivo coincide con este filtro.' : 'Todavía no hay escaneos. El primero se hace a los pocos segundos de arrancar el servicio.'}</div>
        )}
        {rows.length > 0 && pg.pager}
        <p className="small muted" style={{ margin: 0, padding: '12px var(--pad)', borderTop: '1px solid var(--line)' }}>Los nombres salen del propio equipo (Bonjour o NetBIOS){rt.connected ? ', del DHCP del router' : ''} o del DNS del router, y el fabricante, de la MAC. Si un equipo no anuncia su nombre, pónselo tú con «Añadir a Dispositivos» o «Editar». Los iPhone y Android usan una MAC privada distinta para cada red, así que no muestran fabricante.</p>
      </Panel>
    </>
  );
}
