import { useEffect, useState } from 'react';
import type { View } from '../../data/types';
import type { Derived } from '../../state/derive';
import { loadHistory, set, useStore } from '../../state/store';
import { ActButton, Icon, Panel, Seg, act, dur, fmt, gib, hhmm, type Action, type IconName } from '../../design/ui';
import { usePaged } from '../../design/Pager';
import { copySsh, openUrl, sshCommand } from '../../state/actions';
import { peak } from './charts';
import { Chart } from './Chart';
import { SaludDiscos, SistemaOperativo } from './Sistema';
import { Copias } from './Copias';
import { LIVE_POINTS, LIVE_STEP, useLive } from './live';

const XL: Record<View, string[]> = { live: ['−5 min', '−4', '−3', '−2', '−1', 'Ahora'], '1h': ['−60 min', '−45', '−30', '−15', 'Ahora'], '24h': ['−24 h', '−18 h', '−12 h', '−6 h', 'Ahora'], '7d': ['−7 d', '−5 d', '−3 d', '−1 d', 'Hoy'] };
const PCT = ['100 %', '50 %', '0 %'];

export function Servidor({ d }: { d: Derived }) {
  const s = useStore();
  const x = d.s, m = x.metrics;
  const disks = usePaged(m?.disks || [], 'discos');
  // «En vivo» no se recuerda: al salir de Servidor vuelve al historial normal.
  const [live, setLive] = useState(false);
  const view: View = live ? 'live' : s.range;
  const lh = useLive(live && d.srvOn);
  useEffect(() => { if (!live) void loadHistory(s.range); }, [live, s.range, s.lastOk]);
  const h = live ? lh : s.hist[s.range];
  const last = live && lh && lh.times.length ? lh.times.length - 1 : -1;
  const cur = last < 0 || !m ? null : { cpu: lh!.cpu[last], mem: lh!.mem[last], rx: lh!.rx[last], tx: lh!.tx[last], temp: lh!.temp[last] };
  const times = h?.times || [];
  const enough = times.length > 1;
  const when = (t: string) => (live ? 'a las ' + new Date(t).toLocaleTimeString('es') : s.range === '7d' ? new Date(t).toLocaleDateString('es', { weekday: 'long' }) + ' a las ' + hhmm(t) : 'a las ' + hhmm(t));
  const growing = live ? 'Leyendo cada ' + LIVE_STEP + ' s…' : 'El historial se va llenando mientras el servicio está en marcha.';
  const cp = h ? peak(h.cpu, h.times) : null;
  const dp = h ? peak(h.rx, h.times) : null;
  const nmax = h ? Math.max(10, Math.ceil(Math.max(...h.rx, ...h.tx, 0) / 10) * 10) : 10;
  const lead = d.srvOn ? 'Ahora ' : 'Último valor ';
  const common = { times, stepSec: h?.stepSec || 60, range: view, xl: XL[view] };

  const ramGB = m ? Math.round(m.mem.totalMB / 1024) : null;
  const facts = [
    { k: 'Estado', v: d.srvOn ? 'En línea' : 'Sin respuesta desde las ' + d.lastKnown },
    { k: 'Sistema', v: x.server.os || '—' }, { k: 'Kernel', v: x.server.kernel || '—' },
    { k: 'Tiempo activo', v: d.srvOn ? dur(x.server.uptimeSec) : 'Desconocido' },
    { k: 'Carga media (1, 5 y 15 min)', v: d.srvOn ? x.server.load.map((l) => fmt(l, 2)).join(' / ') : 'Sin datos' },
    { k: 'Equipo', v: (x.server.cpuModel || 'Procesador') + ' · ' + x.server.cores + ' núcleos' + (ramGB ? ' · ' + ramGB + ' GB' : '') },
  ];

  const tools: { name: string; desc: string; icon: IconName; act: Action }[] = [];
  if (d.netdata) tools.push({ name: 'Netdata', desc: 'Métricas detalladas. Consume memoria de forma continua: este panel ya cubre lo esencial.', icon: 'activity',
    act: act({ label: 'Abrir', kind: 'external', run: () => openUrl('Netdata', d.netdata!.url || ''), blocked: d.serverBlocked || (d.netdata.state !== 'running' ? 'Netdata está detenido.' : !d.netdata.url ? 'No publica ningún puerto.' : '') }) });
  tools.push({ name: 'Consola de Tailscale', desc: 'Dispositivos de tu red Tailscale, claves y permisos de acceso', icon: 'lock', act: act({ label: 'Abrir', kind: 'external', run: () => openUrl('la consola de Tailscale', 'https://login.tailscale.com/admin/machines') }) });
  tools.push({ name: 'Terminal (SSH)', desc: sshCommand(d), icon: 'terminal', act: act({ label: 'Copiar comando', icon: 'copy', run: () => void copySsh(d) }) });

  return (
    <>
      <dl className="facts">{facts.map((f) => <div className="fact" key={f.k}><dt>{f.k}</dt><dd>{f.v}</dd></div>)}</dl>

      <Panel title="Uso de recursos" id="res-h" hint={enough ? 'Pasa el ratón por una gráfica para ver cada punto' : undefined}
        right={<Seg<View> value={view} options={[['live', 'En vivo'], ['1h', '1 hora'], ['24h', '24 horas'], ['7d', '7 días']]} label="Periodo" onChange={(r) => { if (r === 'live') setLive(true); else { setLive(false); set({ range: r }); } }} />}>
        {live && (
          <p className="live-note" role="status"><span className="live-dot" aria-hidden="true" /><span><b>En vivo.</b> El servidor lee una muestra cada {LIVE_STEP} s y aquí se ven los últimos {Math.round((LIVE_POINTS * LIVE_STEP) / 60)} minutos.{!d.srvOn && ' Ahora mismo está en pausa porque el servidor no responde.'}</span></p>
        )}
        {!m ? (
          <div className="empty"><Icon name="info" />{x.metricsError || 'Aún no hay lecturas del servidor.'}</div>
        ) : (
          <>
            <div className={'charts' + (d.srvOn ? '' : ' stale')}>
              <Chart {...common} title="Procesador" now={lead + Math.round(cur ? cur.cpu : m.cpuPct) + ' %'} max={100} y={PCT}
                series={[{ label: 'CPU', values: h?.cpu || [], color: 'var(--c1)', fmt: (v) => fmt(v) + ' %', area: true }]}
                note={enough && cp?.t ? 'Máximo de ' + Math.round(cp.v) + ' % ' + when(cp.t) + '.' : growing} />
              <Chart {...common} title="Tráfico de red" legend max={nmax} y={[nmax + ' Mbit/s', String(nmax / 2), '0']}
                series={[
                  { label: 'Bajada', now: d.srvOn ? fmt(cur ? cur.rx : m.net.rxMbps) + ' Mbit/s' : '—', values: h?.rx || [], color: 'var(--c1)', fmt: (v) => fmt(v) + ' Mbit/s' },
                  { label: 'Subida', now: d.srvOn ? fmt(cur ? cur.tx : m.net.txMbps) + ' Mbit/s' : '—', values: h?.tx || [], color: 'var(--c2)', fmt: (v) => fmt(v) + ' Mbit/s' },
                ]}
                note={enough && dp?.t ? 'Máximo de bajada: ' + fmt(dp.v) + ' Mbit/s ' + when(dp.t) + '. Interfaz ' + m.net.iface + '.' : 'Interfaz ' + (m.net.iface || '—') + '.'} />
            </div>
            <div className={'charts' + (d.srvOn ? '' : ' stale')} style={{ borderTop: '1px solid var(--line)' }}>
              <Chart {...common} title="Memoria en uso" now={lead + (cur ? Math.round(cur.mem) : d.memPct) + ' %'} max={100} y={PCT}
                series={[{ label: 'Memoria', values: h?.mem || [], color: 'var(--c1)', fmt: (v) => fmt(v) + ' % · ' + gib((v / 100) * m.mem.totalMB) }]}
                note={enough && h ? 'Máximo de ' + Math.round(Math.max(...h.mem)) + ' % ' + (live ? 'en estos minutos' : 'en el periodo') + '. La caché que Linux libera cuando hace falta no cuenta como uso.' : growing} />
              {m.tempC != null ? (
                <Chart {...common} title="Temperatura del procesador" now={lead + Math.round(cur ? cur.temp : m.tempC) + ' °C'} max={100} y={['100 °C', '50 °C', '0 °C']}
                  series={[{ label: 'Temperatura', values: h?.temp || [], color: 'var(--c2)', fmt: (v) => fmt(v) + ' °C' }]}
                  note={(enough && h ? 'Máximo de ' + Math.round(Math.max(...h.temp)) + ' °C ' + (live ? 'en estos minutos' : 'en el periodo') + '. ' : '') + 'Sensor ' + m.tempOf + '. Por encima de 85 °C el panel avisa.'} />
              ) : (
                <div className="chart"><div className="chart-h"><b>Temperatura del procesador</b></div><div className="empty" style={{ padding: 0 }}><Icon name="info" />El servidor no expone ningún sensor de temperatura.</div></div>
              )}
            </div>
            <div style={{ borderTop: '1px solid var(--line)', padding: '18px var(--pad)' }}>
              <div className="chart-h"><b>Memoria</b><span className="chart-now">{d.srvOn ? 'Disponible ' + gib(m.mem.totalMB - m.mem.usedMB) + ' de ' + gib(m.mem.totalMB) : 'Último valor conocido'}</span></div>
              <div className="membar" role="img" aria-label={'Memoria: ' + gib(m.mem.usedMB) + ' en uso, ' + gib(m.mem.cacheMB) + ' de caché liberable y ' + gib(m.mem.freeMB) + ' libres de ' + gib(m.mem.totalMB) + '.'}>
                <span style={{ width: (m.mem.usedMB / m.mem.totalMB) * 100 + '%', background: 'var(--c1)' }} />
                <span style={{ width: (m.mem.cacheMB / m.mem.totalMB) * 100 + '%', background: 'color-mix(in srgb,var(--c1) 35%,transparent)' }} />
              </div>
              <div className="legend" style={{ marginTop: 10 }}>
                <span><i style={{ background: 'var(--c1)' }} />En uso {gib(m.mem.usedMB)}</span>
                <span><i style={{ background: 'color-mix(in srgb,var(--c1) 35%,transparent)' }} />Caché liberable {gib(m.mem.cacheMB)}</span>
                <span><i style={{ background: 'var(--fill)', border: '1px solid var(--line2)' }} />Libre {gib(m.mem.freeMB)}</span>
              </div>
            </div>
          </>
        )}
      </Panel>

      <Panel title="Almacenamiento" id="disk-h" hint="Aviso al 80 %, problema al 90 %">
        {!m?.disks.length ? <div className="empty"><Icon name="info" />Sin datos de discos.</div> : (
          <div className="tbox">
            <table className="tbl">
              <thead><tr><th>Volumen</th><th className="hide-sm">Punto de montaje</th><th style={{ width: '34%' }}>Uso</th><th className="num">Libre</th></tr></thead>
              <tbody>
                {disks.rows.map((v) => {
                  const pct = Math.round((v.usedGB / v.totalGB) * 100);
                  return (
                    <tr key={v.mount}>
                      <td><span className="strong">{v.label}</span><span className="sub">{v.device}</span></td>
                      <td className="hide-sm">{v.mount}</td>
                      <td><div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
                        <div className={'meter' + (pct >= 90 ? ' err' : pct >= 80 ? ' warn' : '')} role="meter" aria-valuemin={0} aria-valuemax={100} aria-valuenow={pct} aria-label={'Uso de ' + v.label}><span style={{ width: pct + '%' }} /></div>
                        <span className="nowrap" style={{ minWidth: 110 }}>{pct + ' %' + (pct >= 80 ? ' · aviso' : '') + (d.srvOn ? '' : ' (último dato)')}</span>
                      </div></td>
                      <td className="num">{Math.round(v.totalGB - v.usedGB)} GB<span className="sub">de {Math.round(v.totalGB)} GB</span></td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )}
        {(m?.disks.length || 0) > 0 && disks.pager}
      </Panel>

      <SaludDiscos d={d} />
      <SistemaOperativo d={d} />
      {d.hasNC && <Copias d={d} />}

      <Panel title="Herramientas de administración" id="tools-h">
        <div>
          {tools.map((t) => (
            <div className="lrow" key={t.name}>
              <span className="ico-box"><Icon name={t.icon} /></span>
              <div className="grow"><span className="strong">{t.name}</span><span className={'sub' + (t.icon === 'terminal' ? ' mono' : '')}>{t.desc}</span></div>
              {t.act.tag && <span className="tag hide-sm">{t.act.tag}</span>}
              <ActButton a={t.act} />
            </div>
          ))}
        </div>
      </Panel>
    </>
  );
}
