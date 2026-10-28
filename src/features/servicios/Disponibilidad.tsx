import type { Derived } from '../../state/derive';
import { set } from '../../state/store';
import { Badge, Empty, Panel, agoIso, dur, fmt, st } from '../../design/ui';
import { usePaged } from '../../design/Pager';

export const APP_TARGET = 'app:nextcloud';
export const targetName = (t: string) => (t === APP_TARGET ? 'Nextcloud (aplicación)' : t);

export function pctLabel(p: number | null) {
  if (p == null) return '—';
  return p >= 99.995 ? '100 %' : fmt(Math.floor(p * 100) / 100, p >= 99 ? 2 : 1) + ' %';
}

/* Disponibilidad de cada servicio en las últimas 24 h y 7 días, y sus caídas.
 * La anota el servidor cada 15 s, aunque nadie mire el panel. */
export function Disponibilidad({ d }: { d: Derived }) {
  const av = d.s.avail || [];
  const inc = d.s.incidents || [];
  const exists = new Set(d.s.docker.containers.map((c) => c.name).concat(d.hasNC ? [APP_TARGET] : []));
  const rows = av.filter((a) => exists.has(a.target)).sort((a, b) => (a.pct7d ?? 101) - (b.pct7d ?? 101));
  const pgA = usePaged(rows, 'disponibilidad');
  const pgI = usePaged(inc, 'caidas');
  const badge = (p: number | null, down: string | null) => down ? st('resErr', 'Caído') : p == null ? st('noCheck', 'Sin datos') : p >= 99.9 ? st('ok', 'Estable') : p >= 99 ? st('warn', 'Alguna caída') : st('resErr', 'Inestable');

  return (
    <>
      <Panel title="Disponibilidad" id="av-h" hint="Tiempo funcionando. Se anota cada 15 s; el modo de mantenimiento no cuenta">
        {rows.length ? (
          <div className="tbox">
            <table className="tbl">
              <thead><tr><th>Servicio</th><th>Estado</th><th className="num">24 horas</th><th className="num">7 días</th><th className="hide-sm">Datos desde</th></tr></thead>
              <tbody>
                {pgA.rows.map((a) => (
                  <tr key={a.target}>
                    <td>{a.target === APP_TARGET ? <span className="strong">{targetName(a.target)}</span>
                      : <button className="name-btn" onClick={() => set({ drawer: { type: 'service', id: a.target } })}><span className="nb-t strong">{a.target}</span></button>}
                      {a.down && <span className="sub" style={{ color: 'var(--err)' }}>Caído {agoIso(a.down).toLowerCase()}</span>}</td>
                    <td><Badge s={badge(a.pct7d, a.down)} /></td>
                    <td className="num">{pctLabel(a.pct24h)}</td>
                    <td className="num">{pctLabel(a.pct7d)}</td>
                    <td className="hide-sm small muted">{a.hours >= 168 ? '7 días completos' : 'Hace ' + dur(a.hours * 3600)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : <Empty icon="activity">El servidor empieza a anotar la disponibilidad en cuanto arranca. Vuelve en unos minutos.</Empty>}
        {rows.length > 0 && pgA.pager}
      </Panel>

      <Panel title="Caídas recientes" id="inc-h" hint="Las de más de 30 segundos; se guardan las 100 últimas">
        {inc.length ? (
          <div className="tbox">
            <table className="tbl">
              <thead><tr><th>Servicio</th><th>Empezó</th><th>Duración</th><th>Estado</th></tr></thead>
              <tbody>
                {pgI.rows.map((i) => {
                  const end = i.to ? new Date(i.to).getTime() : Date.now();
                  return (
                    <tr key={i.target + i.from}>
                      <td className="strong">{targetName(i.target)}</td>
                      <td className="nowrap">{agoIso(i.from)}<span className="sub">{new Date(i.from).toLocaleString('es', { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' })}</span></td>
                      <td className="nowrap">{dur(Math.max(30, (end - new Date(i.from).getTime()) / 1000))}</td>
                      <td><Badge s={i.to ? st('ok', 'Recuperado') : st('resErr', 'Sigue caído')} /></td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        ) : <Empty icon="check">Ninguna caída registrada.</Empty>}
        {inc.length > 0 && pgI.pager}
      </Panel>
    </>
  );
}
