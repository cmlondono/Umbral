import type { Derived } from '../../state/derive';
import { nav, set } from '../../state/store';
import { ActButton, Badge, Icon, Panel, agoIso, st } from '../../design/ui';
import { usePaged } from '../../design/Pager';
import { deviceIcon, deviceSub } from '../red/Red';
import { pcActions } from '../../state/actions';

export function Dispositivos({ d }: { d: Derived }) {
  const x = d.s;
  const pa = pcActions(d);
  const pcBadge = d.pcState === 'notConfigured' ? st('notConfigured') : st(d.pcState);
  const gw = x.network.devices.find((v) => v.ip === x.network.gateway);
  const pinned = x.network.devices.filter((v) => v.pinned && !v.registered);
  const open = (id: string) => () => set({ drawer: { type: 'device', id } });
  const reg = usePaged([
    <tr key="server">
                <td><button className="name-btn" onClick={open('server')}><span className="ico-box"><Icon name="server" /></span><span><span className="nb-t strong">Servidor de casa</span><span className="sub">{x.server.hostname} · {x.server.os.split(' (')[0] || 'Linux'}</span></span></button></td>
                <td><Badge s={st(d.srvOn ? 'online' : 'offline')} /></td>
                <td className="hide-sm nowrap">{x.server.ip || '—'}</td>
                <td className="hide-sm nowrap small">{x.network.devices.find((v) => v.registered === 'server')?.mac || '—'}</td>
                <td className="nowrap">{d.srvOn ? 'Ahora' : 'A las ' + d.lastKnown}</td>
                <td className="num"><div className="row-actions"><button className="btn btn-sm btn-quiet" onClick={open('server')}>Detalles</button></div></td>
              </tr>,
    <tr key="pc">
                <td><button className="name-btn" onClick={open('pc')}><span className="ico-box"><Icon name="monitor" /></span><span><span className="nb-t strong">{x.pc.name || 'PC de escritorio'}</span><span className="sub">Computador personal</span></span></button></td>
                <td><Badge s={pcBadge} /></td>
                <td className="hide-sm nowrap">{x.pc.ip || '—'}</td>
                <td className="hide-sm nowrap small">{x.pc.mac || '—'}</td>
                <td className="nowrap">{d.pcState === 'on' ? 'Ahora' : d.pcState === 'unknown' ? 'Desconocido' : x.pc.lastSeen ? agoIso(x.pc.lastSeen) : '—'}</td>
                <td className="num"><div className="row-actions">
                  <ActButton a={d.pcState === 'on' ? pa.remote : pa.wake} />
                  <button className="btn btn-sm btn-quiet" onClick={open('pc')}>Detalles</button>
                </div></td>
              </tr>,
  ], 'registrados');
  const pg = usePaged(pinned, 'mis-dispositivos');

  return (
    <>
      <Panel title="Equipos registrados" id="reg-h" right={<button className="btn btn-sm" onClick={() => nav('settings', { setTab: 'devices' })}><Icon name="settings" />Editar equipos</button>}>
        <div className="tbox">
          <table className="tbl">
            <thead><tr><th>Dispositivo</th><th>Estado</th><th className="hide-sm">Dirección IP</th><th className="hide-sm">Dirección MAC</th><th>Última vez visto</th><th><span className="sr">Acciones</span></th></tr></thead>
            <tbody>
              {reg.rows}
            </tbody>
          </table>
        </div>
        {reg.pager}
      </Panel>

      <Panel title="Mis dispositivos" id="oth-h" hint="Los que añades desde Red" right={<button className="btn btn-sm" onClick={() => nav('network')}><Icon name="plus" />Añadir dispositivo</button>}>
        {pinned.length ? (
          <div className="tbox">
            <table className="tbl">
              <thead><tr><th>Dispositivo</th><th>Estado</th><th className="hide-sm">Dirección IP</th><th className="hide-sm">Dirección MAC</th><th>Última vez visto</th></tr></thead>
              <tbody>
                {pg.rows.map((v) => (
                  <tr key={v.mac}>
                    <td><span style={{ display: 'flex', alignItems: 'center', gap: 12 }}><span className="ico-box"><Icon name={deviceIcon(v, x.network.gateway)} /></span><span><span className="strong">{v.name || 'Sin nombre'}</span><span className="sub">{deviceSub(v, x.network.gateway)}<span className="show-sm"> · {v.ip}</span></span></span></span></td>
                    <td><Badge s={!d.srvOn ? st('unknown') : v.online ? st('connected') : st('noReply')} /></td>
                    <td className="hide-sm nowrap">{v.ip}</td>
                    <td className="hide-sm nowrap small">{v.mac}</td>
                    <td className="nowrap">{v.online ? 'Ahora' : agoIso(v.lastSeen)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : (
          <div className="empty"><Icon name="info" /><span>Todo lo que se conecta a tu red aparece solo en Red. Para seguir uno aquí (el iPhone, la TV, la impresora…), búscalo en Red y pulsa «Añadir a Dispositivos». <button className="link-btn" onClick={() => nav('network')}>Ir a Red</button></span></div>
        )}
        {pinned.length > 0 && pg.pager}
        <div className="lrow" style={{ borderTop: '1px solid var(--line)' }}>
          <button className="name-btn grow" onClick={open('router')}><span className="ico-box"><Icon name="router" /></span><span style={{ minWidth: 0 }}><span className="nb-t strong">Router principal</span><span className="sub">Puerta de enlace · {x.network.gateway || '—'}</span></span></button>
          <Badge s={!d.srvOn || !gw ? st('unknown') : gw.online ? st('appOk') : st('noReply')} />
        </div>
      </Panel>
    </>
  );
}
