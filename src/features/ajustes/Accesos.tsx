import { useEffect, useState } from 'react';
import type { AccessEvent } from '../../data/types';
import { adapter, handleErr, useStore } from '../../state/store';
import { Badge, Empty, Icon, Panel, Seg, agoIso, st, type StateInfo } from '../../design/ui';
import { usePaged } from '../../design/Pager';

const KIND: Record<AccessEvent['kind'], [string, StateInfo]> = {
  login: ['Entrada', st('ok', 'Correcta')],
  fail: ['Contraseña o código incorrectos', st('warn', 'Fallida')],
  locked: ['Bloqueo de 15 min por intentos fallidos', st('resErr', 'Bloqueado')],
  logout: ['Salida', st('noCheck', 'Cerrada')],
  password: ['Cambio de contraseña', st('ok', 'Hecho')],
  'totp-on': ['Doble factor activado', st('ok', 'Hecho')],
  'totp-off': ['Doble factor desactivado', st('warn', 'Hecho')],
};

/* Registro de accesos al panel: se lee del servidor al abrir la pestaña y
 * con cada «Actualizar». */
export function Accesos() {
  const lastOk = useStore().lastOk;
  const [list, setList] = useState<AccessEvent[] | null>(null);
  const [filter, setFilter] = useState<'all' | 'problems'>('all');
  useEffect(() => {
    let off = false;
    adapter().accessLog().then((l) => { if (!off) setList(l); }).catch((e) => { if (!off) { handleErr(e, 'No se pudo leer el registro de accesos'); setList([]); } });
    return () => { off = true; };
  }, [lastOk]);
  const problems = (list || []).filter((e) => e.kind === 'fail' || e.kind === 'locked' || e.new);
  const rows = filter === 'problems' ? problems : list || [];
  const pg = usePaged(rows, 'accesos');

  return (
    <Panel title="Registro de accesos" hint="Se guardan los 300 más recientes"
      right={<Seg label="Filtrar accesos" value={filter} onChange={setFilter} counts={{ all: list?.length || 0, problems: problems.length }} options={[['all', 'Todos'], ['problems', 'A revisar']]} />}>
      {!list ? <Empty icon="clock">Leyendo…</Empty> : rows.length ? (
        <div className="tbox">
          <table className="tbl">
            <thead><tr><th>Cuándo</th><th>Qué</th><th>Resultado</th><th className="hide-sm">Desde</th></tr></thead>
            <tbody>
              {pg.rows.map((e, i) => (
                <tr key={e.at + i}>
                  <td className="nowrap">{agoIso(e.at)}<span className="sub">{new Date(e.at).toLocaleString('es', { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' })}</span></td>
                  <td><span className="strong">{KIND[e.kind][0]}</span><span className="sub">{e.user ? 'Usuario ' + e.user : 'Usuario desconocido'}{e.new ? ' · primera vez desde este equipo' : ''}</span></td>
                  <td><Badge s={e.new ? st('warn', 'Equipo nuevo') : KIND[e.kind][1]} /></td>
                  <td className="hide-sm"><span className="mono small">{e.ip}</span><span className="sub">{e.agent}</span></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : <Empty icon={filter === 'problems' ? 'shield' : 'clock'}>{filter === 'problems' ? 'Nada que revisar: ni intentos fallidos ni entradas desde equipos nuevos.' : 'Aún no hay accesos registrados.'}</Empty>}
      {rows.length > 0 && pg.pager}
      <p className="small muted" style={{ margin: 0, padding: '12px var(--pad)', borderTop: '1px solid var(--line)' }}><Icon name="bell" style={{ width: 14, height: 14, verticalAlign: '-2px' }} /> Para enterarte al momento, crea una automatización con el disparador «Acceso al panel» y la acción «Avisar al móvil».</p>
    </Panel>
  );
}
