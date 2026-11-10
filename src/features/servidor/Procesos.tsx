import { Fragment, useState } from 'react';
import type { CPUPeak, CPUSplit, ProcUse } from '../../data/types';
import type { Derived } from '../../state/derive';
import { adapter, run } from '../../state/store';
import { Badge, Empty, Icon, Panel, Seg, agoIso, dur, fmt, st, usePlegado } from '../../design/ui';
import { usePaged } from '../../design/Pager';
import type { LiveHistory } from './live';

/* Qué usa el procesador y qué causó cada pico. El servidor lee /proc cada
 * 15 s y agrupa los procesos por programa y origen. */

export function origin(p: ProcUse) {
  switch (p.kind) {
    case 'container': return 'Contenedor ' + p.group;
    case 'service': return p.group && p.group !== p.name ? 'Servicio ' + p.group : 'Servicio del sistema';
    case 'user': return 'Sesión de usuario (SSH o terminal)';
    case 'kernel': return 'Núcleo de Linux';
    default: return 'Sistema';
  }
}

/* Qué suele ser cada programa conocido. Solo orienta: el origen manda. */
const HINTS: [RegExp, string][] = [
  [/^php/, 'Código PHP de la aplicación. En Nextcloud suelen ser las tareas en segundo plano (cron), las vistas previas o la sincronización.'],
  [/^(apache2|httpd|nginx|caddy)$/, 'Servidor web: atiende peticiones, por ejemplo de los clientes de sincronización.'],
  [/^(mariadbd|mysqld|postgres|mongod)$/, 'Base de datos: consultas pesadas o tareas de mantenimiento.'],
  [/^(mariadb-dump|mysqldump|pg_dump|gzip)$/, 'Copia de la base de datos.'],
  [/^redis/, 'Caché en memoria. Rara vez pesa.'],
  [/^(dockerd|containerd|containerd-shim|runc|docker-proxy)/, 'Docker: arranque de contenedores, descarga de imágenes o comprobaciones de salud.'],
  [/^kswapd/, 'Falta memoria: Linux mueve memoria al disco. Mira la gráfica de memoria.'],
  [/^kworker$/, 'Trabajo interno del núcleo: escrituras a disco, red o controladores.'],
  [/^ksoftirqd$/, 'Interrupciones: mucho tráfico de red o de disco.'],
  [/^(jbd2|kcompactd|khugepaged)/, 'Mantenimiento del sistema de archivos o de la memoria.'],
  [/^(apt|dpkg|unattended-upgr|http)/, 'Actualizaciones de Debian.'],
  [/^rsync$/, 'Copia de archivos.'],
  [/^smartctl$/, 'Lectura de la salud de los discos.'],
  [/^(ffmpeg|convert|magick|imagick)/, 'Procesado de imágenes o vídeo, por ejemplo vistas previas.'],
  [/^umbral$/, 'Este panel.'],
  [/^tailscaled$/, 'Tailscale: tráfico por la red privada.'],
  [/^sshd/, 'Conexiones SSH.'],
];
export const hint = (p: ProcUse) => HINTS.find(([r]) => r.test(p.name.toLowerCase()))?.[1] || '';

const pct = (v: number) => (v < 10 ? fmt(v) : String(Math.round(v))) + ' %';
const span = (sec: number) => (sec < 60 ? Math.round(sec) + ' s' : sec < 3600 ? Math.round(sec / 60) + ' min' : dur(sec));
const when = (iso: string) => new Date(iso).toLocaleString('es', { weekday: 'short', day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' });

function SplitBar({ s }: { s: CPUSplit }) {
  const parts = [
    { k: 'Programas', v: s.user, c: 'var(--c1)' },
    { k: 'Núcleo', v: s.system + s.steal, c: 'color-mix(in srgb,var(--c1) 40%,transparent)' },
    { k: 'Espera de disco', v: s.iowait, c: 'var(--warn)' },
  ];
  return (
    <>
      <div className="membar" role="img" aria-label={'Reparto del procesador: ' + parts.map((p) => p.k + ' ' + pct(p.v)).join(', ') + '.'}>
        {parts.map((p) => <span key={p.k} style={{ width: p.v + '%', background: p.c }} />)}
      </div>
      <div className="legend" style={{ marginTop: 10 }}>
        {parts.map((p) => <span key={p.k}><i style={{ background: p.c }} />{p.k} {pct(p.v)}</span>)}
      </div>
    </>
  );
}

/** Lectura de qué pesa: avisa si domina el disco o el núcleo. */
function splitNote(s: CPUSplit) {
  if (s.iowait >= 15) return 'Mucha espera de disco: el disco va lento o está saturado. Esa espera no cuenta en la gráfica, pero frena el servidor.';
  if (s.system > s.user && s.system >= 20) return 'Domina el núcleo: suele ser disco, red o falta de memoria, más que un programa concreto.';
  return '';
}

function Meter({ v }: { v: number }) {
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
      <div className={'meter' + (v >= 50 ? ' warn' : '')} role="meter" aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(v)} aria-label="Uso del procesador"><span style={{ width: Math.min(100, v) + '%' }} /></div>
      <span className="nowrap" style={{ minWidth: 52 }}>{pct(v)}</span>
    </div>
  );
}

function ProcTable({ rows }: { rows: ProcUse[] }) {
  return (
    <div className="tbox">
      <table className="tbl">
        <thead><tr><th>Programa</th><th className="hide-sm">Qué suele ser</th><th style={{ width: '26%' }}>Procesador</th><th className="num hide-sm">Memoria</th></tr></thead>
        <tbody>
          {rows.map((p) => (
            <tr key={p.kind + p.group + p.name}>
              <td><span className="strong mono">{p.name}</span>{p.count > 1 && <span className="muted small"> × {p.count}</span>}<span className="sub">{origin(p)}</span></td>
              <td className="hide-sm small muted">{hint(p) || '—'}</td>
              <td><Meter v={p.cpu} /></td>
              <td className="num hide-sm">{p.memMB ? p.memMB + ' MB' : '—'}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export function ProcesosAhora({ d, live }: { d: Derived; live: LiveHistory | null }) {
  const cpu = d.s.cpu;
  const now = cpu?.now;
  const rows = live && live.top.length ? live.top : now?.procs || [];
  const pg = usePaged(rows, 'procesos');
  const note = now && !live ? splitNote(now.split) : '';
  const hintText = !d.srvOn ? 'Último dato conocido' : live ? 'En vivo · cada 2 s' : now ? 'Media de los últimos 15 s · ' + now.tasks + ' procesos' : undefined;
  const fold = usePlegado('procesos');
  const first = rows[0];
  const summary = cpu?.error ? cpu.error : !first ? 'Aún sin datos: el servidor necesita dos lecturas.'
    : 'Lo que más usa ahora: ' + first.name + ' (' + origin(first).toLowerCase() + '), ' + pct(first.cpu) + ' del procesador.';

  return (
    <Panel title="Qué usa el procesador" id="procs-h" hint={hintText} fold={fold} summary={summary}>
      {cpu?.error ? <Empty icon="info">{cpu.error}</Empty> : !rows.length ? (
        <Empty icon="clock">El servidor necesita dos lecturas para calcularlo. Vuelve en 15 segundos.</Empty>
      ) : (
        <>
          {now && !live && (
            <div style={{ padding: '14px var(--pad)', borderBottom: '1px solid var(--line)' }}>
              <SplitBar s={now.split} />
              {note && <p className="small" style={{ margin: '10px 0 0' }}><Icon name="info" className="i i-sm" /> {note}</p>}
            </div>
          )}
          <ProcTable rows={pg.rows} />
          {pg.pager}
          <p className="small muted" style={{ margin: 0, padding: '12px var(--pad)', borderTop: '1px solid var(--line)' }}>
            El porcentaje es del procesador entero, como en la gráfica: 100 % son todos los núcleos a la vez. Los procesos con el mismo nombre y origen se suman.
          </p>
        </>
      )}
    </Panel>
  );
}

function PeakRow({ p, open, toggle }: { p: CPUPeak; open: boolean; toggle: () => void }) {
  const top = p.top || [];
  const main = top[0];
  const secs = (new Date(p.to).getTime() - new Date(p.from).getTime()) / 1000;
  const note = splitNote(p.split);
  const id = 'pk-' + p.from;
  return (
    <>
      <tr>
        <td>{agoIso(p.from)}<span className="sub">{when(p.from)} · {span(secs)}<span className="show-sm"> · máx. {pct(p.max)}</span></span></td>
        <td className="nowrap hide-sm">Máx. {pct(p.max)}<span className="sub">media {pct(p.avg)}</span></td>
        <td>{main ? <><span className="strong mono">{main.name}</span> <span className="muted small">{pct(main.cpu)}</span><span className="sub">{origin(main)}</span></> : '—'}</td>
        <td className="hide-sm"><Badge s={p.open ? st('warn', 'En curso') : st('ok', 'Terminado')} /></td>
        <td className="num">
          <button className="btn btn-sm" aria-expanded={open} aria-controls={id} onClick={toggle}>{open ? 'Ocultar' : 'Detalle'}</button>
        </td>
      </tr>
      {open && (
        <tr id={id}>
          <td colSpan={5} style={{ whiteSpace: 'normal', padding: '14px var(--pad)', background: 'color-mix(in srgb,var(--fill) 35%,transparent)' }}>
            <SplitBar s={p.split} />
            {note && <p className="small" style={{ margin: '10px 0 0' }}><Icon name="info" className="i i-sm" /> {note}</p>}
            <div style={{ marginTop: 12 }}>
              {top.map((t) => (
                <div key={t.kind + t.group + t.name} style={{ display: 'grid', gridTemplateColumns: 'minmax(0,1fr) minmax(120px,30%)', gap: 12, alignItems: 'center', padding: '6px 0' }}>
                  <div style={{ minWidth: 0 }}>
                    <span className="strong mono">{t.name}</span>{t.count > 1 && <span className="muted small"> × {t.count}</span>}
                    <span className="sub">{origin(t)}{t.memMB ? ' · ' + t.memMB + ' MB' : ''}</span>
                    {hint(t) && <span className="sub">{hint(t)}</span>}
                  </div>
                  <Meter v={t.cpu} />
                </div>
              ))}
            </div>
            <p className="small muted" style={{ margin: '8px 0 0' }}>Media de cada programa durante el pico, con {p.n} {p.n === 1 ? 'lectura' : 'lecturas'} de 15 s.</p>
          </td>
        </tr>
      )}
    </>
  );
}

export function PicosCPU({ d }: { d: Derived }) {
  const cpu = d.s.cpu;
  const peaks = cpu?.peaks || [];
  const pg = usePaged(peaks, 'picos');
  const [open, setOpen] = useState<string | null>(null);
  const fold = usePlegado('picos');
  if (!cpu || cpu.error) return null;
  const last = peaks[0];
  const cause = last?.top?.[0];
  const summary = !last ? 'Ningún pico por encima del ' + cpu.threshold + ' % desde que Umbral los anota.'
    : (last.open ? 'Hay un pico en curso' : 'El último pico fue ' + agoIso(last.from).toLowerCase()) + ': máx. ' + pct(last.max) + (cause ? ', sobre todo por ' + cause.name : '') + '. '
      + peaks.length + (peaks.length === 1 ? ' pico guardado.' : ' picos guardados.');
  const setThr = (v: string) => void run('No se pudo cambiar el umbral', () => adapter().setPeakThreshold(Number(v)));

  return (
    <Panel title="Picos del procesador" id="peaks-h" fold={fold} summary={summary} hint={'Se anota cuando pasa del ' + cpu.threshold + ' % y qué programas lo causaron. Se guardan 30 días (como mucho 100)'}
      right={d.serverBlocked ? <span className="hint">{d.serverBlocked}</span>
        : <Seg<string> value={String(cpu.threshold)} label="Umbral de los picos" options={[['50', '50 %'], ['70', '70 %'], ['90', '90 %']]} onChange={setThr} />}>
      {peaks.length ? (
        <>
          <div className="tbox">
            <table className="tbl">
              <thead><tr><th>Cuándo</th><th className="hide-sm">Procesador</th><th>Causa principal</th><th className="hide-sm">Estado</th><th className="num"><span className="sr">Detalle</span></th></tr></thead>
              <tbody>
                {pg.rows.map((p) => <Fragment key={p.from}><PeakRow p={p} open={open === p.from} toggle={() => setOpen(open === p.from ? null : p.from)} /></Fragment>)}
              </tbody>
            </table>
          </div>
          {pg.pager}
        </>
      ) : <Empty icon="check">Ningún pico por encima del {cpu.threshold} % desde que Umbral los anota.</Empty>}
    </Panel>
  );
}
