import { useEffect } from 'react';
import type { Derived } from '../../state/derive';
import { loadHistory, nav, set, useStore } from '../../state/store';
import { Badge, Icon, LinkBtn, Panel, act, dur, fmt, gib, glyphPath, st, type IconName, type StateInfo } from '../../design/ui';
import { openUrl, pcActions } from '../../state/actions';
import { openWizard } from '../servicios/Wizard';
import { sparkPath } from '../servidor/charts';

interface Node { name: string; sub: string; icon: IconName; badge: StateInfo; go: () => void; dim?: boolean; style?: React.CSSProperties }

function TopoNode({ n }: { n: Node }) {
  return (
    <button className={'node' + (n.dim ? ' is-off' : '')} style={n.style} onClick={n.go}>
      <span className="ico-box"><Icon name={n.icon} /></span>
      <span style={{ minWidth: 0 }}><span className="node-t">{n.name}</span><span className="node-s">{n.sub}</span><Badge s={n.badge} /></span>
    </button>
  );
}

export function Inicio({ d }: { d: Derived }) {
  const s = useStore();
  const x = d.s;
  useEffect(() => { void loadHistory('24h'); }, [s.lastOk]);
  const tone = ({ ok: ['check', 't-ok'], warn: ['alert', 't-warn'], err: ['x', 't-err'] } as const)[d.level];

  const gw = x.network.devices.find((v) => v.ip === x.network.gateway);
  const nodes = {
    internet: { name: 'Internet', sub: 'Conexión del hogar', icon: 'globe', badge: !d.srvOn || x.internet == null ? st('unknown') : x.internet ? st('connected', 'Conectada') : st('offline', 'Sin conexión'), go: () => nav('network') } as Node,
    router: { name: 'Router', sub: x.network.gateway || 'Puerta de enlace', icon: 'router', badge: !d.srvOn || !gw ? st('unknown') : gw.online ? st('appOk') : st('noReply'), go: () => set({ drawer: { type: 'device', id: 'router' } }) } as Node,
    server: { name: 'Servidor', sub: d.srvOn ? (x.docker.available ? d.running + ' de ' + d.ctrs.length + ' servicios activos' : 'Docker sin respuesta') : 'Último dato a las ' + d.lastKnown, icon: 'server', badge: st(d.srvOn ? 'online' : 'offline'), go: () => nav('server') } as Node,
    nc: { name: 'Nextcloud', sub: d.ncUrl.replace(/^https?:\/\//, '') || 'Sin dirección', icon: 'cloud', badge: d.ncState, go: () => nav('services'), dim: d.srvOn && !!d.nc && d.nc.state !== 'running' } as Node,
    pc: { name: x.pc.name || 'PC de escritorio', sub: x.pc.ip || 'Sin configurar', icon: 'monitor', badge: d.pcState === 'notConfigured' ? st('notConfigured') : st(d.pcState), go: () => set({ drawer: { type: 'device', id: 'pc' } }), dim: d.pcState !== 'on' } as Node,
  };
  const link = (on: boolean) => 'tl' + (on ? '' : ' dash');

  const m = x.metrics;
  const h = s.hist['24h'];
  const pa = pcActions(d);
  const ncOpen = act({ label: 'Abrir Nextcloud', kind: 'external', run: () => openUrl('Nextcloud', d.ncUrl),
    blocked: d.serverBlocked || (!d.nc ? 'No se encontró el contenedor de Nextcloud.' : d.nc.state !== 'running' ? 'Nextcloud está detenido.' : !d.ncUrl ? 'Nextcloud no publica ningún puerto.' : ''),
    note: d.srvOn && d.nc?.app === 'appFail' ? 'Ahora mismo no responde bien.' : d.ncUrl.replace(/^https?:\/\//, '') });
  const deployA = act({ label: 'Desplegar un servicio', icon: 'plus', run: () => openWizard(), blocked: d.serverBlocked || (!x.docker.available ? 'Docker no responde.' : ''), note: 'Desde el catálogo, un docker-compose o una imagen' });
  const quick = d.hasNC ? [pa.wake, ncOpen, pa.remote, deployA, pa.check] : [pa.wake, pa.remote, deployA, pa.check];

  const metrics: { label: string; icon: IconName; value: string; sub: string; spark?: number[]; max?: number; pct?: number; meter?: string; aria: string }[] = m ? [
    { label: 'Procesador', icon: 'cpu', value: Math.round(m.cpuPct) + ' %', sub: 'carga ' + fmt(x.server.load[0], 2), spark: h?.cpu, max: 100, aria: 'Uso del procesador en las últimas 24 horas' },
    { label: 'Memoria', icon: 'memory', value: gib(m.mem.usedMB), sub: 'de ' + gib(m.mem.totalMB) + ' en uso', pct: d.memPct, meter: 'meter' + (d.memPct >= 90 ? ' err' : d.memPct >= 80 ? ' warn' : ''), aria: 'Memoria' },
    ...(d.diskMax ? [{ label: 'Almacenamiento', icon: 'disk' as IconName, value: d.diskMax.pct + ' %', sub: d.diskMax.label.toLowerCase() + ', ' + d.diskMax.freeGB + ' GB libres', pct: d.diskMax.pct, meter: 'meter' + (d.diskMax.pct >= 90 ? ' err' : d.diskMax.pct >= 80 ? ' warn' : ''), aria: 'Almacenamiento' }] : []),
    ...(m.tempC != null ? [{ label: 'Temperatura', icon: 'activity' as IconName, value: Math.round(m.tempC) + ' °C', sub: 'procesador', spark: h?.temp, max: 100, aria: 'Temperatura del procesador en las últimas 24 horas' }] : []),
    { label: 'Red', icon: 'updown', value: '↓ ' + fmt(m.net.rxMbps) + '  ↑ ' + fmt(m.net.txMbps), sub: 'Mbit/s', spark: h?.rx, aria: 'Tráfico de bajada en las últimas 24 horas' },
  ] : [];

  return (
    <>
      <section className="hero" aria-live="polite">
        <div className={'hero-ic ' + tone[1]}><svg className="i" viewBox="0 0 24 24" aria-hidden="true"><path d={glyphPath(tone[0])} /></svg></div>
        <div><h2>{d.title}</h2><p>{d.detail}</p></div>
      </section>

      <Panel title="Tu ecosistema" id="map-h" hint="Selecciona un elemento para ver sus detalles">
        <div className="topo">
          <TopoNode n={{ ...nodes.internet, style: { gridColumn: 1, gridRow: 1 } }} />
          <div className={link(d.srvOn && x.internet === true)} style={{ gridColumn: 2, gridRow: 1 }} aria-hidden="true" />
          <TopoNode n={{ ...nodes.router, style: { gridColumn: 3, gridRow: 1 } }} />
          <div className={link(d.srvOn)} style={{ gridColumn: 4, gridRow: 1 }} aria-hidden="true" />
          <TopoNode n={{ ...nodes.server, style: { gridColumn: 5, gridRow: 1 } }} />
          {d.hasNC && <div className={link(d.ncUsable)} style={{ gridColumn: 6, gridRow: 1 }} aria-hidden="true" />}
          {d.hasNC && <TopoNode n={{ ...nodes.nc, style: { gridColumn: 7, gridRow: 1 } }} />}
          <div className={'tl-l' + (d.pcState === 'on' ? '' : ' dash')} aria-hidden="true"><div /></div>
          <TopoNode n={{ ...nodes.pc, style: { gridColumn: 5, gridRow: 3 } }} />
        </div>
      </Panel>

      <div className="home-grid">
        <div className="col">
          <Panel title="Requiere atención" id="att-h" hint={d.alerts.length ? (d.alerts.length === 1 ? '1 elemento' : d.alerts.length + ' elementos') : 'Sin avisos'}>
            {d.alerts.length ? (
              <div>
                {d.alerts.map((a) => (
                  <div className="alert" key={a.title}>
                    <svg className={'i sym b-' + a.sev} viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="10" /><path d={glyphPath(a.sev === 'err' ? 'x' : 'alert')} /></svg>
                    <div style={{ flex: 1, minWidth: 0 }}>
                      <div className="alert-t">{a.title}</div>
                      <div className="alert-d">{a.detail}</div>
                      <LinkBtn onClick={() => { nav(a.page, a.extra); if (a.drawer) set({ drawer: a.drawer }); }}>{a.action}</LinkBtn>
                    </div>
                  </div>
                ))}
              </div>
            ) : (
              <div className="empty"><Icon name="check" style={{ color: 'var(--ok)' }} />Nada que revisar. Los avisos aparecerán aquí cuando algo necesite tu atención.</div>
            )}
          </Panel>

          <Panel title="Servidor ahora" id="now-h" right={<LinkBtn onClick={() => nav('server')}>Ver servidor</LinkBtn>}>
            {!d.srvOn && <div className="banner warn" style={{ margin: 'var(--pad) var(--pad) 4px' }}><Icon name="clock" /><div><b>Últimos valores conocidos ({d.lastKnown})</b><span>No son lecturas actuales.</span></div></div>}
            {!m ? (
              <div className="empty"><Icon name="info" />{x.metricsError || 'Aún no hay lecturas: la primera llega a los 15 segundos de arrancar el servicio.'}</div>
            ) : (
              <div className={d.srvOn ? '' : 'stale'}>
                {metrics.map((r) => (
                  <div className="mrow" key={r.label}>
                    <div className="m-lbl"><Icon name={r.icon} />{r.label}</div>
                    <div><span className="m-val">{r.value}</span><span className="sub">{r.sub}</span></div>
                    <div>
                      {r.spark !== undefined ? (r.spark.length > 1 ? (
                        <svg className="spark" viewBox="0 0 132 30" preserveAspectRatio="none" role="img" aria-label={r.aria}>
                          <path d={sparkPath(r.spark, r.max ?? Math.max(1, ...r.spark), 132, 30, true)} fill="var(--accentSoft)" stroke="none" />
                          <path d={sparkPath(r.spark, r.max ?? Math.max(1, ...r.spark), 132, 30)} fill="none" stroke="var(--c1)" strokeWidth="1.5" vectorEffect="non-scaling-stroke" />
                        </svg>
                      ) : <span className="sub">Reuniendo historial…</span>) : null}
                      {r.pct !== undefined && <div className={r.meter} role="meter" aria-valuemin={0} aria-valuemax={100} aria-valuenow={r.pct} aria-label={r.aria}><span style={{ width: r.pct + '%' }} /></div>}
                    </div>
                  </div>
                ))}
              </div>
            )}
          </Panel>
        </div>

        <div className="col">
          <Panel title="Acciones rápidas" id="qa-h">
            <div>
              {quick.map((q) => (
                <button key={q.label} className={'qa' + (q.kind === 'setup' ? ' setup' : '')} onClick={q.blocked ? undefined : q.run} disabled={!!q.blocked}>
                  <span className="ico-box"><Icon name={q.spin ? 'loader' : q.icon} className={q.spin ? 'i spin' : 'i'} /></span>
                  <span style={{ flex: 1, minWidth: 0 }}><span className="qa-t">{q.label}</span><span className="qa-s">{q.note}</span></span>
                  {q.tag && <span className="tag">{q.tag}</span>}
                </button>
              ))}
            </div>
          </Panel>

          <Panel title="Equipos" id="eq-h" right={<LinkBtn onClick={() => nav('devices')}>Ver todos</LinkBtn>}>
            <div>
              <div className="lrow">
                <button className="name-btn grow" onClick={() => set({ drawer: { type: 'device', id: 'server' } })}>
                  <span className="ico-box"><Icon name="server" /></span>
                  <span style={{ minWidth: 0 }}><span className="nb-t strong">Servidor de casa</span><span className="sub">{d.srvOn ? (x.server.os.split(' (')[0] || 'Debian') + ' · activo ' + dur(x.server.uptimeSec) : 'Último dato a las ' + d.lastKnown}</span></span>
                </button>
                <Badge s={st(d.srvOn ? 'online' : 'offline')} />
              </div>
              <div className="lrow">
                <button className="name-btn grow" onClick={() => set({ drawer: { type: 'device', id: 'pc' } })}>
                  <span className="ico-box"><Icon name="monitor" /></span>
                  <span style={{ minWidth: 0 }}><span className="nb-t strong">{x.pc.name || 'PC de escritorio'}</span><span className="sub">{d.pcState === 'notConfigured' ? 'Añade su IP y MAC en Ajustes' : d.pcState === 'on' ? 'Visto ahora' : d.pcState === 'unknown' ? 'Estado desconocido' : 'Visto por última vez ' + (x.pc.lastSeen ? agoLower(x.pc.lastSeen) : 'hace tiempo')}</span></span>
                </button>
                <Badge s={nodes.pc.badge} />
              </div>
            </div>
          </Panel>
        </div>
      </div>
    </>
  );
}

function agoLower(iso: string) {
  const m = Math.max(0, (Date.now() - new Date(iso).getTime()) / 60000);
  if (m < 1) return 'ahora';
  if (m < 60) return 'hace ' + Math.round(m) + ' min';
  if (m < 1440) return 'hace ' + Math.floor(m / 60) + ' h';
  const dd = Math.floor(m / 1440); return 'hace ' + dd + (dd === 1 ? ' día' : ' días');
}
