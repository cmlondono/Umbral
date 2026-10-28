import { useDialog } from '../design/useDialog';
import type { Derived } from '../state/derive';
import { dismiss, get, nav, set, useStore } from '../state/store';
import { ActButton, Badge, Icon, Sym, act, agoIso, dur, fmt, memLabel, st, type Action, type IconName, type StateInfo } from '../design/ui';
import { backupDb, copySsh, ctrAction, imgRef, openUrl, pcActions, removeImages, startPull } from '../state/actions';
import { Wizard, openWizardForImage } from '../features/servicios/Wizard';
import { ContainerExtra, LogsModal } from '../features/servicios/ContainerExtra';
import { pctLabel } from '../features/servicios/Disponibilidad';

interface DrawerView {
  icon: IconName; title: string; subtitle: string; badge: StateInfo; second?: StateInfo; note?: string;
  facts: { k: string; v: string }[];
  groups: { title: string; desc: string; actions: Action[] }[];
  extra?: React.ReactNode;
}

function deviceDrawer(id: string, d: Derived): DrawerView {
  const s = d.s;
  if (id === 'pc') {
    const a = pcActions(d);
    const pc = s.pc;
    return { icon: 'monitor', title: pc.name || 'PC de escritorio', subtitle: 'Computador personal' + (pc.ip ? ' · ' + pc.ip : ''), badge: d.pcState === 'notConfigured' ? st('notConfigured') : st(d.pcState),
      facts: [
        { k: 'Dirección IP', v: pc.ip || 'Sin configurar' }, { k: 'Dirección MAC', v: pc.mac || 'Sin configurar' },
        { k: 'Última vez visto', v: d.pcState === 'on' ? 'Ahora' : pc.lastSeen ? agoIso(pc.lastSeen) : 'Todavía no se ha visto' },
        { k: 'Último encendido', v: pc.lastWake ? agoIso(pc.lastWake) + ' desde el panel' : 'Ninguno desde el panel' },
        { k: 'Cómo se comprueba', v: 'Conexión TCP al puerto ' + s.config.pc.checkPort + ' y tabla ARP del servidor' },
        { k: 'Wake-on-LAN', v: pc.mac ? 'Lo envía ' + s.server.hostname + ' a la difusión de la red local.' : 'Falta la MAC' },
      ],
      groups: [
        { title: 'Acciones', desc: 'Se ejecutan desde el servidor.', actions: [a.wake, a.check] },
        { title: 'Requieren un servicio externo', desc: 'El panel abre la herramienta; la conexión la gestiona ella.', actions: [a.remote] },
      ] };
  }
  if (id === 'server') {
    return { icon: 'server', title: 'Servidor de casa', subtitle: 'Servidor doméstico · ' + s.server.hostname, badge: st(d.srvOn ? 'online' : 'offline'),
      note: d.srvOn ? '' : 'No llegan datos desde las ' + d.lastKnown + '. Los valores son los últimos conocidos.',
      facts: [
        { k: 'Sistema operativo', v: s.server.os || '—' }, { k: 'Dirección IP', v: s.server.ip || '—' },
        { k: 'Tailscale', v: s.tailscale.self ? s.tailscale.self.dnsName + ' · ' + s.tailscale.self.ip : 'Sin conectar' },
        { k: 'Tiempo activo', v: d.srvOn ? dur(s.server.uptimeSec) : 'Desconocido' },
        { k: 'Equipo', v: (s.server.cpuModel || 'Procesador') + ' · ' + s.server.cores + ' núcleos' + (s.metrics ? ' · ' + Math.round(s.metrics.mem.totalMB / 1024) + ' GB' : '') },
      ],
      groups: [
        { title: 'Acciones', desc: '', actions: [act({ label: 'Ver detalles del servidor', run: () => nav('server') }), act({ label: 'Copiar comando SSH', icon: 'copy', run: () => void copySsh(d) })] },
      ] };
  }
  const r = s.network.devices.find((x) => x.ip === s.network.gateway);
  return { icon: 'router', title: 'Router principal', subtitle: 'Puerta de enlace · ' + (s.network.gateway || '—'), badge: !d.srvOn || !r ? st('unknown') : r.online ? st('appOk') : st('noReply'),
    facts: [{ k: 'Dirección IP', v: s.network.gateway || '—' }, { k: 'Dirección MAC', v: r?.mac || '—' }, { k: 'Última detección', v: r ? (r.online ? 'Ahora' : agoIso(r.lastSeen)) : '—' }],
    groups: [
      { title: 'Acciones', desc: '', actions: [act({ label: 'Ver dispositivos de la red', run: () => nav('network') })] },
    ] };
}

function serviceDrawer(name: string, d: Derived): DrawerView | null {
  const c = d.ctrs.find((k) => k.name === name);
  if (!c) return null;
  const busy = c.shown === 'restarting' || c.shown === 'stopping' || c.shown === 'starting' || c.shown === 'removing';
  const off = d.serverBlocked || (busy ? 'Espera a que termine la acción en curso.' : '');
  const acts: Action[] = [];
  if (c.url) acts.push(act({ label: 'Abrir ' + c.name, kind: 'external', run: () => openUrl(c.name, c.url!), blocked: off || (c.state !== 'running' ? 'El contenedor no está en ejecución.' : '') }));
  if (c.state !== 'running') {
    acts.push(act({ label: 'Iniciar contenedor', icon: 'play', kind: 'primary', run: () => ctrAction(c, 'start'), blocked: off }));
    acts.push(act({ label: 'Eliminar contenedor', icon: 'trash', kind: 'danger', run: () => ctrAction(c, 'remove'), blocked: off, note: 'Los datos montados y la imagen se conservan.' }));
  } else {
    if (c.isNextcloud && d.hasNC) acts.push(act({ label: 'Copia de seguridad de la base de datos', icon: 'download', run: () => void backupDb(c.name), blocked: off || (get().busyBackup ? 'Haciendo la copia…' : ''), note: 'Se guarda en ' + d.s.config.dataRoot + '/copias. Nextcloud sigue funcionando mientras tanto.' }));
    acts.push(act({ label: 'Reiniciar contenedor', icon: 'restart', run: () => ctrAction(c, 'restart'), blocked: off }));
    acts.push(act({ label: 'Detener contenedor', icon: 'stop', kind: 'danger', run: () => ctrAction(c, 'stop'), blocked: off }));
  }
  const appDetail = c.isNextcloud ? d.ncAppDetail
    : { healthy: 'La comprobación de salud de Docker pasa', unhealthy: 'La comprobación de salud de Docker falla', healthStarting: 'La comprobación de salud aún no ha terminado', unknown: 'Sin datos: el servidor no responde', na: 'El contenedor no está en ejecución' }[c.app as string] || 'Este contenedor no tiene comprobación de salud';
  const note = !d.srvOn ? 'Último estado conocido a las ' + d.lastKnown + '.'
    : c.isNextcloud && c.app === 'appFail' ? 'El contenedor funciona, pero la aplicación no.' + (d.s.nextcloud.maintenance ? ' Reiniciar el contenedor no desactiva el modo de mantenimiento: desactívalo con occ maintenance:mode --off.' : '')
    : c === d.netdata && c.state === 'running' ? 'Netdata consume memoria y CPU de forma continua. Si no lo consultas, detenlo y después elimina el contenedor y su imagen: este panel ya muestra lo esencial.'
    : c.name.endsWith('-anterior') ? 'Es la versión anterior de un contenedor actualizado. Se conserva para poder revertir; elimínala cuando confirmes que la nueva funciona.' : '';
  return { icon: c.isNextcloud ? 'cloud' : 'boxes', title: c.name, subtitle: c.image + (c.compose ? ' · pila ' + c.compose : ''), badge: st(c.shown), second: st(c.app, c.appLabel), note,
    facts: [
      { k: 'Contenedor', v: c.status || st(c.shown).label }, { k: 'Aplicación', v: appDetail },
      { k: 'Imagen', v: c.image }, { k: 'Puertos publicados', v: c.ports.length ? c.ports.join(', ') : 'Ninguno (solo red interna de Docker)' },
      { k: 'Consumo', v: c.state === 'running' && c.cpuPct != null ? fmt(c.cpuPct) + ' % de CPU · ' + c.memMB + ' MB de memoria' : c.state === 'running' ? 'Se mide mientras el panel está abierto' : 'Sin datos' },
      { k: 'Dirección', v: c.url || 'Sin puerto publicado' },
      ...(() => { const a = d.s.avail?.find((x) => x.target === c.name); return a ? [{ k: 'Disponibilidad', v: pctLabel(a.pct24h) + ' en 24 h · ' + pctLabel(a.pct7d) + ' en 7 días' }] : []; })(),
    ],
    groups: [{ title: 'Acciones', desc: 'Reiniciar, detener y eliminar piden confirmación.', actions: acts }],
    extra: d.srvOn && d.s.docker.available ? <ContainerExtra key={c.name} name={c.name} memMB={c.state === 'running' ? c.memMB : null} blocked={off} /> : undefined };
}

function imageDrawer(id: string, d: Derived): DrawerView {
  const im = d.s.docker.images.find((i) => i.id + i.repo + i.tag === id || i.id === id);
  if (!im) return { icon: 'boxes', title: 'Imagen eliminada', subtitle: '', badge: st('noCheck', 'Ya no está en el servidor'), facts: [], groups: [] };
  const dangling = im.repo === '<none>';
  const deploy = act({ label: 'Desplegar esta imagen', icon: 'play', kind: 'primary', run: () => openWizardForImage(im), blocked: d.serverBlocked || (dangling ? 'Una imagen sin etiqueta no se puede desplegar.' : '') });
  const del = act({ label: 'Eliminar imagen', icon: 'trash', kind: 'danger', run: () => removeImages([im]), blocked: d.serverBlocked || (im.usedBy.length ? 'La usa ' + im.usedBy.join(', ') + '. Elimina antes ese contenedor.' : ''), note: im.usedBy.length ? '' : 'Liberará ' + memLabel(im.sizeMB) + '.' });
  return { icon: 'boxes', title: dangling ? 'Imagen sin etiqueta' : im.repo, subtitle: dangling ? 'ID ' + im.id : 'Etiqueta ' + im.tag, badge: imgBadge(im, d),
    note: dangling ? 'Suele ser la versión anterior de una imagen que se actualizó. Si nada la usa, se puede borrar.' : '',
    facts: [
      { k: 'Referencia', v: dangling ? 'Sin etiqueta' : imgRef(im) }, { k: 'ID', v: 'sha256:' + im.id + '…' }, { k: 'Tamaño', v: memLabel(im.sizeMB) },
      { k: 'Creada', v: agoIso(im.created) }, { k: 'Arquitectura', v: 'linux/' + (im.arch || '—') },
      { k: 'Puerto que expone', v: im.exposedPort ? im.exposedPort + '/tcp' : 'Ninguno declarado' },
      { k: 'Carpetas que declara', v: im.volumes.length ? im.volumes.join(', ') : 'Ninguna' },
      { k: 'Usada por', v: im.usedBy.length ? im.usedBy.join(', ') : 'Ningún contenedor' },
    ],
    groups: [{ title: 'Acciones', desc: 'Desplegar abre el asistente con los puertos y carpetas de esta imagen ya rellenados.', actions: [deploy, del] }] };
}

export function imgBadge(im: { repo: string; usedBy: string[] }, d: Derived): StateInfo {
  if (im.repo === '<none>') return st('warn', 'Sin etiqueta');
  if (!im.usedBy.length) return st('noCheck', 'Sin usar');
  const running = im.usedBy.some((n) => d.ctrs.find((c) => c.name === n)?.state === 'running');
  return running ? st('ok', 'En uso') : st('off', 'En uso, detenida');
}

export function Layers({ d }: { d: Derived | null }) {
  const s = useStore();
  const drawer = d && s.drawer ? (s.drawer.type === 'device' ? deviceDrawer(s.drawer.id, d) : s.drawer.type === 'image' ? imageDrawer(s.drawer.id, d) : serviceDrawer(s.drawer.id, d)) : null;
  const closeDrawer = () => set({ drawer: null });
  const drawerRef = useDialog(!!drawer && !s.modal && !s.logs, closeDrawer);
  const modalRef = useDialog(!!s.modal, () => set({ modal: null }));

  return (
    <>
      {drawer && (
        <>
          <div className="scrim" onClick={closeDrawer} />
          <aside className="drawer glass" role="dialog" aria-modal="true" aria-labelledby="dr-title" ref={drawerRef as React.RefObject<HTMLElement>}>
            <div className="dr-h">
              <span className="ico-box" style={{ width: 40, height: 40 }}><Icon name={drawer.icon} /></span>
              <div style={{ flex: 1, minWidth: 0 }}>
                <h2 id="dr-title">{drawer.title}</h2>
                <span className="sub" style={{ marginBottom: 6 }}>{drawer.subtitle}</span>
                <Badge s={drawer.badge} />
                {drawer.second && <span style={{ marginLeft: 12 }}><Badge s={drawer.second} /></span>}
              </div>
              <button className="btn btn-quiet btn-icon" onClick={closeDrawer} aria-label="Cerrar detalles"><Icon name="close" /></button>
            </div>
            {drawer.note && <div className="banner warn" style={{ margin: '16px 22px 0' }}><Icon name="alert" /><div><span>{drawer.note}</span></div></div>}
            {drawer.facts.length > 0 && <dl className="dl">{drawer.facts.map((f) => <div key={f.k}><dt>{f.k}</dt><dd>{f.v}</dd></div>)}</dl>}
            {drawer.groups.filter((g) => g.actions.length).map((g) => (
              <div className="ag" key={g.title}>
                <h3>{g.title}</h3>
                {g.desc && <p>{g.desc}</p>}
                <div className="ag-list">
                  {g.actions.map((a) => (
                    <div className="ag-item" key={a.label}>
                      <ActButton a={a} small={false} showTitle={false} />
                      {a.note && <span className="ag-note">{a.note}</span>}
                    </div>
                  ))}
                </div>
              </div>
            ))}
            {drawer.extra}
            <div style={{ height: 20 }} />
          </aside>
        </>
      )}

      {s.pull && <PullModal />}
      {s.logs && <LogsModal name={s.logs} />}
      {s.wiz && d && <Wizard d={d} />}

      {s.modal && (
        <>
          <div className="scrim" style={{ zIndex: 50 }} onClick={() => set({ modal: null })} />
          <div className="modal-wrap" onClick={() => set({ modal: null })}>
            <div className="modal glass" role="alertdialog" aria-modal="true" aria-labelledby="md-title" aria-describedby="md-body" onClick={(e) => e.stopPropagation()} ref={modalRef}>
              <h2 id="md-title">{s.modal.title}</h2>
              <p id="md-body">{s.modal.body}</p>
              <div className="modal-f">
                {s.modal.secondaryLabel && <button className="btn btn-quiet" onClick={s.modal.onSecondary} style={{ marginRight: 'auto' }}>{s.modal.secondaryLabel}</button>}
                {!s.modal.noCancel && <button className="btn" onClick={() => set({ modal: null })}>Cancelar</button>}
                <button className={s.modal.confirmCls || 'btn btn-primary'} onClick={s.modal.onConfirm}>{s.modal.confirmLabel}</button>
              </div>
            </div>
          </div>
        </>
      )}

      <div className="toasts" aria-live="polite">
        {s.toasts.map((t) => {
          const m = ({ ok: ['check', 'b-ok'], info: ['info', 'b-busy'], warn: ['alert', 'b-warn'], err: ['x', 'b-err'] } as const)[t.tone];
          return (
            <div className="toast glass" role="status" key={t.id}>
              <Sym glyph={m[0] === 'info' ? 'alert' : m[0]} className={'i sym ' + m[1]} />
              <div className="grow"><div className="toast-t">{t.title}</div><div className="toast-b">{t.body}</div></div>
              <button className="btn btn-quiet btn-icon btn-sm" style={{ width: 28, height: 28 }} onClick={() => dismiss(t.id)} aria-label="Cerrar aviso"><Icon name="close" className="i i-sm" /></button>
            </div>
          );
        })}
      </div>
    </>
  );
}

const SUGGEST = ['vaultwarden/server:latest', 'adguard/adguardhome:latest', 'binwiederhier/ntfy:latest', 'nginx:1.27-alpine'];
const validImage = (t: string) => /^[a-z0-9]+([._/:-][a-z0-9]+)*(:[A-Za-z0-9._-]+)?$/.test(t.trim());

function PullModal() {
  const s = useStore();
  const p = s.pull!;
  const nm = p.name.trim(), ok = validImage(nm);
  const local = ok && s.snap?.docker.images.some((i) => imgRef(i) === (nm.includes(':') ? nm : nm + ':latest'));
  const close = () => set({ pull: null });
  const ref = useDialog(true, p.phase === 'pulling' ? undefined : close);
  const job = p.job;
  const title = p.phase === 'done' ? 'Imagen descargada' : p.phase === 'pulling' ? 'Descargando imagen…' : p.phase === 'error' ? 'No se pudo descargar' : 'Descargar una imagen';
  const deployNow = () => {
    const im = s.snap?.docker.images.find((i) => imgRef(i) === p.ref);
    if (im) { set({ pull: null }); openWizardForImage(im); }
  };
  return (
    <>
      <div className="scrim" style={{ zIndex: 50 }} />
      <div className="modal-wrap">
        <div className="modal glass" role="dialog" aria-modal="true" aria-labelledby="pl-t" style={{ width: 'min(500px,100%)' }} ref={ref}>
          <h2 id="pl-t">{title}</h2>
          {p.phase === 'form' && (
            <>
              <p>Escribe el nombre de la imagen como aparece en Docker Hub o en otro registro, como ghcr.io.</p>
              <div className="field" style={{ marginTop: 16 }}>
                <label className="f-l" htmlFor="pl-n">Imagen</label>
                <input id="pl-n" className={'inp mono' + (nm && !ok ? ' bad' : '')} placeholder="por ejemplo, vaultwarden/server:latest" value={p.name}
                  onChange={(e) => set({ pull: { ...p, name: e.target.value } })} onKeyDown={(e) => { if (e.key === 'Enter' && ok) void startPull(); }} spellCheck={false} autoComplete="off" />
                <span className={'f-msg' + (nm && !ok ? ' bad' : '')}>{nm && !ok ? 'Formato no válido. Usa nombre:etiqueta, por ejemplo, nginx:1.27-alpine.' : local ? 'Ya la tienes. Se buscará una versión más nueva con la misma etiqueta.' : 'Si no indicas etiqueta, se usará latest.'}</span>
              </div>
              <div className="field" style={{ marginTop: 14 }}>
                <span className="f-l">Sugerencias</span>
                <div className="chips">{SUGGEST.map((r) => <button key={r} className="chip mono" onClick={() => set({ pull: { ...p, name: r } })}>{r}</button>)}</div>
              </div>
            </>
          )}
          {p.phase === 'pulling' && (
            <>
              <p className="mono">{p.ref}</p>
              <div className="meter" style={{ height: 8, marginTop: 16 }} role="progressbar" aria-valuemin={0} aria-valuemax={100} aria-valuenow={job?.progress || 0} aria-label="Progreso de la descarga"><span style={{ width: (job?.progress || 0) + '%', transition: 'width 260ms' }} /></div>
              <p className="f-msg" style={{ marginTop: 8 }}>{job?.message || 'Preparando la descarga…'}</p>
            </>
          )}
          {p.phase === 'done' && (
            <div className="banner ok" style={{ marginTop: 6 }}><Icon name="check" /><div><b className="mono">{p.ref}</b><span>{p.existed ? 'Ya tenías la versión más reciente de esta etiqueta, o se ha actualizado.' : 'Ya aparece en la lista de imágenes, sin usar.'}</span></div></div>
          )}
          {p.phase === 'error' && (
            <div className="banner err" style={{ marginTop: 6 }}><Icon name="x" /><div><b className="mono">{p.ref}</b><span>{job?.message}</span></div></div>
          )}
          <div className="modal-f">
            {p.phase === 'form' && <><button className="btn" onClick={close}>Cancelar</button><button className="btn btn-primary" onClick={() => void startPull()} disabled={!ok}>Descargar</button></>}
            {p.phase === 'pulling' && <span className="f-msg">La descarga sigue en el servidor aunque cierres el panel.</span>}
            {p.phase === 'done' && <><button className="btn" onClick={close}>Cerrar</button><button className="btn btn-primary" onClick={deployNow}>Desplegar ahora</button></>}
            {p.phase === 'error' && <><button className="btn" onClick={close}>Cerrar</button><button className="btn btn-primary" onClick={() => set({ pull: { ...p, phase: 'form', job: null } })}>Probar otra vez</button></>}
          </div>
        </div>
      </div>
    </>
  );
}
