import type { Derived } from '../../state/derive';
import { adapter, run, toast } from '../../state/store';
import { Badge, Banner, Empty, Icon, Panel, agoIso, st, type StateInfo } from '../../design/ui';
import { usePaged } from '../../design/Pager';
import type { SmartDisk } from '../../data/types';

const UPGRADE = 'sudo apt update && sudo apt full-upgrade';

async function copy(text: string, what: string) {
  try { await navigator.clipboard.writeText(text); toast({ tone: 'ok', title: what + ' copiada', body: 'Pégala en la terminal del servidor.' }); }
  catch { toast({ tone: 'info', title: 'Cópiala a mano', body: text }); }
}

function CheckButton({ d }: { d: Derived }) {
  const sys = d.s.system;
  const pending = !!sys.requested;
  const check = () => void run('No se pudo comprobar', () => adapter().systemCheck(), sys.helperInstalled ? { title: 'Comprobando', body: 'El servidor descarga la lista de paquetes y lee los discos. Tarda uno o dos minutos.' } : undefined);
  return (
    <button className="btn btn-sm" onClick={check} disabled={!!d.serverBlocked || pending} title={d.serverBlocked || (pending ? 'Ya se está comprobando' : 'Descarga la lista de paquetes y vuelve a leer la salud de los discos')}>
      <Icon name="refresh" />{pending ? 'Comprobando…' : 'Comprobar ahora'}
    </button>
  );
}

/* Actualizaciones de Debian: el servidor las calcula sin instalarlas. */
export function SistemaOperativo({ d }: { d: Derived }) {
  const sys = d.s.system;
  const pg = usePaged(sys.packages || [], 'paquetes');
  if (!sys.aptAvailable) return null;
  const n = sys.packages?.length || 0;
  const badge: StateInfo = sys.aptError ? st('resErr', 'No se pudo calcular') : !n ? st('ok', 'Al día') : sys.security ? st('warn', n + ' pendientes · ' + sys.security + ' de seguridad') : st('warn', n + ' pendientes');
  const listsOld = !sys.listsAt || Date.now() - new Date(sys.listsAt).getTime() > 2 * 86400000;

  return (
    <Panel title="Sistema operativo" id="os-h" hint={d.s.server.os || undefined}
      right={<div style={{ display: 'flex', gap: 10, alignItems: 'center', flexWrap: 'wrap' }}><Badge s={badge} /><CheckButton d={d} /></div>}>
      {sys.reboot && (
        <Banner tone="warn" icon="restart" title="Hay que reiniciar el servidor" style={{ margin: 'var(--pad) var(--pad) 0' }}>
          Una actualización ya instalada ({sys.rebootPkgs?.join(', ') || 'del sistema'}) no se aplica del todo hasta reiniciar. Hazlo cuando nadie use los servicios: <code>sudo reboot</code>
        </Banner>
      )}
      {!sys.helperInstalled && (
        <Banner tone="setup" icon="wrench" title="Falta el ayudante de sistema" style={{ margin: 'var(--pad) var(--pad) 0' }}>
          Sin él, la lista de paquetes solo se renueva cuando la actualizas tú. Vuelve a ejecutar <code>sudo sh instalar.sh</code> con esta versión de Umbral.
        </Banner>
      )}
      <div className="setrow"><div className="grow"><span className="strong">Lista de paquetes</span>
        <span className="sub">{sys.listsAt ? 'Descargada ' + agoIso(sys.listsAt).toLowerCase() + '.' : 'Sin descargar.'}{listsOld ? ' Está desactualizada: pulsa «Comprobar ahora».' : ''}{sys.listsUpdate && !sys.listsUpdate.ok ? ' La última descarga falló: ' + sys.listsUpdate.error : ''}</span></div>
        <Badge s={listsOld ? st('warn', 'Antigua') : st('ok', 'Reciente')} /></div>
      <div className="setrow"><div className="grow"><span className="strong">Actualizaciones automáticas de seguridad</span>
        <span className="sub">{sys.unattended ? 'Debian instala solo los parches de seguridad cada día (unattended-upgrades).' : 'Desactivadas. Para que Debian instale solo los parches de seguridad: sudo apt install unattended-upgrades && sudo dpkg-reconfigure -plow unattended-upgrades'}</span></div>
        <Badge s={sys.unattended ? st('ok', 'Activadas') : st('noCheck', 'Desactivadas')} /></div>
      {sys.aptError && <p className="f-msg bad" style={{ padding: '0 var(--pad)' }}>{sys.aptError}</p>}
      {n > 0 && (
        <>
          <div className="tbox" style={{ borderTop: '1px solid var(--line)' }}>
            <table className="tbl">
              <thead><tr><th>Paquete</th><th className="hide-sm">Versión actual</th><th>Versión nueva</th><th>Tipo</th></tr></thead>
              <tbody>
                {pg.rows.map((p) => (
                  <tr key={p.name}>
                    <td className="strong mono">{p.name}</td>
                    <td className="hide-sm mono small">{p.from || 'nuevo'}</td>
                    <td className="mono small">{p.to}</td>
                    <td><Badge s={p.security ? st('warn', 'Seguridad') : st('noCheck', 'Normal')} /></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {pg.pager}
          <div className="lrow" style={{ borderTop: '1px solid var(--line)' }}>
            <span className="ico-box"><Icon name="terminal" /></span>
            <div className="grow"><span className="strong">Instalarlas por SSH</span><span className="sub mono">{UPGRADE}</span></div>
            <button className="btn btn-sm" onClick={() => void copy(UPGRADE, 'Orden')}><Icon name="copy" />Copiar orden</button>
          </div>
        </>
      )}
      <p className="small muted" style={{ margin: 0, padding: '12px var(--pad)', borderTop: '1px solid var(--line)' }}>
        {sys.aptCheckedAt ? 'Calculado ' + agoIso(sys.aptCheckedAt).toLowerCase() + '. ' : ''}Umbral solo informa: instalar paquetes del sistema necesita permisos de administrador y conviene hacerlo a mano.
      </p>
    </Panel>
  );
}

function diskBadge(k: SmartDisk): StateInfo {
  if (!k.supported) return st('noCheck', 'Sin datos');
  return { ok: st('ok', 'Sano'), warn: st('warn', 'Desgaste'), fail: st('resErr', 'Fallando'), unknown: st('noCheck', 'Sin datos') }[k.health];
}

function diskDetail(k: SmartDisk) {
  if (!k.supported) return k.note;
  const parts: string[] = [];
  if (k.hours != null) parts.push('Encendido ' + (k.hours >= 8760 ? (k.hours / 8760).toFixed(1).replace('.', ',') + ' años' : Math.round(k.hours / 24) + ' días'));
  if (k.percentUsed != null) parts.push('desgaste ' + k.percentUsed + ' %');
  if (k.reallocated) parts.push(k.reallocated + ' sectores reasignados');
  if (k.pending) parts.push(k.pending + ' sectores pendientes');
  if (k.uncorrectable) parts.push(k.uncorrectable + ' sectores ilegibles');
  if (k.mediaErrors) parts.push(k.mediaErrors + ' errores de lectura');
  if (k.passed === false) parts.push('la autocomprobación del disco falla');
  return parts.join(' · ') + (k.note ? '. ' + k.note : '');
}

/* Salud de los discos (SMART), leída por el ayudante de root. */
export function SaludDiscos({ d }: { d: Derived }) {
  const sys = d.s.system;
  const smart = sys.smart;
  return (
    <Panel title="Salud de los discos" id="smart-h" hint={smart?.available ? 'Leída ' + agoIso(smart.at).toLowerCase() + ' · cada 6 h' : 'SMART: avisa antes de que un disco falle'}
      right={sys.helperInstalled && smart?.available ? <CheckButton d={d} /> : undefined}>
      {!sys.helperInstalled ? (
        <Empty icon="disk">Para leer la salud de los discos falta el ayudante de sistema. Vuelve a ejecutar <code>sudo sh instalar.sh</code> con esta versión de Umbral.</Empty>
      ) : !smart ? (
        <Empty icon="clock">El ayudante aún no ha leído los discos. Tarda unos minutos tras instalar.</Empty>
      ) : !smart.available ? (
        <div className="lrow">
          <span className="ico-box"><Icon name="disk" /></span>
          <div className="grow"><span className="strong">Instala smartmontools</span><span className="sub mono">sudo apt install smartmontools</span><span className="sub">Después pulsa «Comprobar ahora». Ocupa unos 2 MB y no deja nada en marcha.</span></div>
          <button className="btn btn-sm" onClick={() => void copy('sudo apt install smartmontools', 'Orden')}><Icon name="copy" />Copiar orden</button>
          <CheckButton d={d} />
        </div>
      ) : !smart.disks.length ? <Empty icon="disk">No se encontró ningún disco.</Empty> : (
        <div>
          {smart.disks.map((k) => (
            <div className="lrow" key={k.name}>
              <span className="ico-box"><Icon name="disk" /></span>
              <div className="grow" style={{ minWidth: 0 }}>
                <span className="strong">{k.model || k.name}</span>
                <span className="sub">{[k.name, k.kind === 'nvme' ? 'NVMe' : k.kind === 'ssd' ? 'SSD' : k.kind === 'hdd' ? 'Disco duro' : '', k.sizeGB ? (k.sizeGB >= 1000 ? (k.sizeGB / 1000).toFixed(1).replace('.', ',') + ' TB' : k.sizeGB + ' GB') : '', k.tempC != null ? k.tempC + ' °C' : ''].filter(Boolean).join(' · ')}</span>
                <span className="sub" style={k.health === 'fail' ? { color: 'var(--err)' } : undefined}>{diskDetail(k)}</span>
              </div>
              <Badge s={diskBadge(k)} />
            </div>
          ))}
        </div>
      )}
    </Panel>
  );
}
