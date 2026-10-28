import type { Derived } from '../../state/derive';
import { adapter, run, set, useStore } from '../../state/store';
import { ActButton, Badge, Empty, Icon, Panel, Search, Seg, act, agoIso, fmt, memLabel, st } from '../../design/ui';
import { usePaged } from '../../design/Pager';
import { DockerSpace } from './DockerSpace';
import { Disponibilidad } from './Disponibilidad';
import { doUpdate, openUrl, removeImages } from '../../state/actions';
import { imgBadge } from '../../app/Layers';
import { openWizard, openWizardForImage } from './Wizard';

export function Servicios({ d }: { d: Derived }) {
  const s = useStore();
  const imgs = d.s.docker.images;
  return (
    <>
      <div>
        <Seg role="tablist" label="Apartados de servicios" value={s.svcTab} onChange={(v) => set({ svcTab: v })}
          options={[['containers', 'Contenedores'], ['images', 'Imágenes'], ['updates', 'Actualizaciones'], ['avail', 'Disponibilidad']]}
          counts={{ containers: d.ctrs.length, images: imgs.length, updates: d.updatesAvail }} />
      </div>
      {!d.s.docker.available && d.srvOn && (
        <div className="banner err" role="status"><Icon name="x" /><div><b>Docker no responde</b><span>{d.s.docker.error}</span></div></div>
      )}
      {s.svcTab === 'containers' && <Contenedores d={d} />}
      {s.svcTab === 'images' && <Imagenes d={d} />}
      {s.svcTab === 'updates' && <Actualizaciones d={d} />}
      {s.svcTab === 'avail' && <Disponibilidad d={d} />}
    </>
  );
}

function Contenedores({ d }: { d: Derived }) {
  const s = useStore();
  const nc = d.nc;
  const ncOpen = act({ label: 'Abrir', kind: 'external', run: () => openUrl('Nextcloud', d.ncUrl), blocked: d.serverBlocked || (!nc ? 'No se encontró el contenedor.' : nc.state !== 'running' ? 'Nextcloud está detenido.' : !d.ncUrl ? 'No publica ningún puerto.' : '') });
  const group = nc ? d.ctrs.filter((c) => c.compose && c.compose === nc.compose).map((c) => c.name) : [];
  const cnt = { all: d.ctrs.length, running: d.ctrs.filter((c) => c.state === 'running').length, issues: d.ctrs.filter((c) => c.issue).length, stopped: d.ctrs.filter((c) => c.state !== 'running').length };
  const q = s.search.trim().toLowerCase();
  const rows = d.ctrs.filter((c) => {
    if (s.svcFilter === 'running' && c.state !== 'running') return false;
    if (s.svcFilter === 'issues' && !c.issue) return false;
    if (s.svcFilter === 'stopped' && c.state === 'running') return false;
    return !q || (c.name + ' ' + c.image).toLowerCase().includes(q);
  });
  const pg = usePaged(rows, 'contenedores');
  const deployBlocked = d.serverBlocked || (!d.s.docker.available ? 'Docker no responde.' : '');

  return (
    <>
      {d.hasNC && <section className="panel" aria-labelledby="nc-h">
        <div className="nc">
          <div>
            <div style={{ display: 'flex', gap: 12, alignItems: 'center' }}>
              <span className="ico-box" style={{ width: 40, height: 40 }}><Icon name="cloud" /></span>
              <div><h2 id="nc-h" style={{ margin: 0, fontSize: 17, fontWeight: 650 }}>Nextcloud</h2><span className="sub">{d.s.nextcloud.version ? 'Versión ' + d.s.nextcloud.version + ' · ' : ''}{d.ncUrl.replace(/^https?:\/\//, '') || 'Sin dirección'}</span></div>
            </div>
            <p className="small muted" style={{ margin: '12px 0 0', maxWidth: '42ch' }}>Un contenedor en ejecución no garantiza que Nextcloud funcione. Por eso se comprueba también la respuesta de la aplicación.</p>
          </div>
          <div>
            <span className="nc-k">Contenedor</span>
            <Badge s={!d.srvOn ? st('unknown') : !nc ? st('notConfigured', 'No encontrado') : st(nc.shown)} />
            <span className="sub" style={{ marginTop: 4 }}>{nc ? (group.length > 1 ? group.join(', ') : nc.name) : 'Indica su nombre en Ajustes › Integraciones'}</span>
          </div>
          <div>
            <span className="nc-k">Aplicación</span>
            <Badge s={!d.srvOn ? st('unknown') : !nc ? st('na') : nc.state !== 'running' ? st('na') : d.ncState} />
            <span className="sub" style={{ marginTop: 4 }}>{d.ncAppDetail}</span>
          </div>
          <div className="nc-actions">
            <ActButton a={ncOpen} small={false} />
            <button className="btn" onClick={() => nc && set({ drawer: { type: 'service', id: nc.name } })} disabled={!nc}>Ver detalles</button>
          </div>
        </div>
      </section>}

      <Panel title="Contenedores Docker" id="svc-h" hint={<span className="hint" style={{ display: 'block' }}>{d.srvOn ? d.running + ' de ' + d.ctrs.length + ' en ejecución' : 'Último estado conocido (' + d.lastKnown + ')'}</span>}
        right={<button className="btn btn-primary btn-sm" onClick={() => openWizard()} disabled={!!deployBlocked} title={deployBlocked || 'Desplegar desde el catálogo, un docker-compose o una imagen'}><Icon name="plus" />Desplegar servicio</button>}>
        <div className="toolbar">
          <Seg label="Filtrar servicios" value={s.svcFilter} onChange={(v) => set({ svcFilter: v })} counts={cnt}
            options={[['all', 'Todos'], ['running', 'En ejecución'], ['issues', 'Con problemas'], ['stopped', 'Detenidos']]} />
          <Search label="Buscar servicio" placeholder="Buscar por nombre o imagen" value={s.search} onChange={(v) => set({ search: v })} />
        </div>
        {rows.length ? (
          <div className={'tbox' + (d.srvOn ? '' : ' stale')}>
            <table className="tbl">
              <thead><tr><th>Servicio</th><th>Contenedor</th><th>Aplicación</th><th className="num hide-sm">CPU</th><th className="num hide-sm">Memoria</th><th className="hide-sm">Puertos</th><th><span className="sr">Acciones</span></th></tr></thead>
              <tbody>
                {pg.rows.map((c) => (
                  <tr key={c.id}>
                    <td><button className="name-btn" onClick={() => set({ drawer: { type: 'service', id: c.name } })}><span><span className="nb-t strong">{c.name}</span><span className="sub mono">{c.image}</span></span></button></td>
                    <td><Badge s={st(c.shown)} /></td>
                    <td><Badge s={st(c.app, c.appLabel)} /></td>
                    <td className="num hide-sm">{c.state === 'running' && c.cpuPct != null ? fmt(c.cpuPct) + ' %' : '—'}</td>
                    <td className="num hide-sm">{c.state === 'running' && c.memMB != null ? c.memMB + ' MB' : '—'}</td>
                    <td className="hide-sm small">{c.ports.length ? c.ports.join(', ') : '—'}</td>
                    <td><div className="row-actions">
                      {c.url && <button className="btn btn-sm btn-quiet btn-icon" onClick={() => openUrl(c.name, c.url!)} aria-label={'Abrir ' + c.name} title={'Abrir ' + c.name} disabled={!d.srvOn || c.state !== 'running'}><Icon name="external" /></button>}
                      <button className="btn btn-sm" onClick={() => set({ drawer: { type: 'service', id: c.name } })}>Gestionar</button>
                    </div></td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : d.ctrs.length ? (
          <Empty icon="search">Ningún servicio coincide con este filtro. <button className="link-btn" onClick={() => set({ svcFilter: 'all', search: '' })}>Mostrar todos</button></Empty>
        ) : (
          <Empty icon="boxes">{d.s.docker.available ? 'No hay contenedores en el servidor. Despliega el primero con el botón de arriba.' : 'Sin datos de Docker.'}</Empty>
        )}
        {rows.length > 0 && pg.pager}
      </Panel>
    </>
  );
}

function Imagenes({ d }: { d: Derived }) {
  const s = useStore();
  const imgs = d.s.docker.images;
  const unused = imgs.filter((i) => !i.usedBy.length);
  const reclaim = unused.reduce((n, i) => n + i.sizeMB, 0);
  // Varias etiquetas de la misma imagen ocupan el disco una sola vez.
  const total = Object.values(Object.fromEntries(imgs.map((i) => [i.id, i.sizeMB]))).reduce((n, v) => n + v, 0);
  const cnt = { all: imgs.length, used: imgs.length - unused.length, unused: unused.length };
  const q = s.imgSearch.trim().toLowerCase();
  const rows = imgs.filter((i) => {
    if (s.imgFilter === 'used' && !i.usedBy.length) return false;
    if (s.imgFilter === 'unused' && i.usedBy.length) return false;
    return !q || (i.repo + ':' + i.tag).toLowerCase().includes(q);
  });
  const pg = usePaged(rows, 'imagenes');
  const blocked = d.serverBlocked || (!d.s.docker.available ? 'Docker no responde.' : '');

  return (
    <>
    <Panel title="Imágenes en el servidor" id="img-h"
      hint={<span className="hint" style={{ display: 'block' }}>{(d.srvOn ? '' : 'Último estado conocido. ') + imgs.length + ' imágenes, ' + memLabel(total) + ' en el disco' + (reclaim ? '. Se pueden liberar ' + memLabel(reclaim) + '.' : '.')}</span>}
      right={<div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
        <button className="btn btn-sm" onClick={() => removeImages(unused)} disabled={!!blocked || !unused.length} title={blocked || (unused.length ? 'Eliminar las imágenes que ningún contenedor usa' : 'Todas las imágenes están en uso')}><Icon name="trash" />{reclaim ? 'Liberar ' + memLabel(reclaim) : 'Nada que liberar'}</button>
        <button className="btn btn-primary btn-sm" onClick={() => set({ pull: { phase: 'form', name: '', ref: '', job: null, existed: false } })} disabled={!!blocked} title={blocked || undefined}><Icon name="download" />Descargar imagen</button>
      </div>}>
      <div className="toolbar">
        <Seg label="Filtrar imágenes" value={s.imgFilter} onChange={(v) => set({ imgFilter: v })} counts={cnt} options={[['all', 'Todas'], ['used', 'En uso'], ['unused', 'Sin usar']]} />
        <Search label="Buscar imagen" placeholder="Buscar por nombre o etiqueta" value={s.imgSearch} onChange={(v) => set({ imgSearch: v })} />
      </div>
      {rows.length ? (
        <div className={'tbox' + (d.srvOn ? '' : ' stale')}>
          <table className="tbl">
            <thead><tr><th>Imagen</th><th>Estado</th><th className="hide-sm">Usada por</th><th className="num hide-sm">Tamaño</th><th className="hide-sm">Creada</th><th><span className="sr">Acciones</span></th></tr></thead>
            <tbody>
              {pg.rows.map((i) => {
                const dangling = i.repo === '<none>', key = i.id + i.repo + i.tag;
                const dep = act({ label: 'Desplegar', run: () => openWizardForImage(i), blocked: blocked || (dangling ? 'Una imagen sin etiqueta no se puede desplegar.' : ''), note: 'Crear un contenedor nuevo con esta imagen' });
                return (
                  <tr key={key}>
                    <td><button className="name-btn" onClick={() => set({ drawer: { type: 'image', id: key } })}><span style={{ minWidth: 0 }}><span className="nb-t strong mono" style={{ display: 'block' }}>{dangling ? 'Sin etiqueta' : i.repo}</span><span className="sub">{dangling ? 'ID ' + i.id : 'Etiqueta ' + i.tag}</span></span></button></td>
                    <td><Badge s={imgBadge(i, d)} /></td>
                    <td className="hide-sm small mono">{i.usedBy.length ? i.usedBy.join(', ') : '—'}</td>
                    <td className="num hide-sm">{memLabel(i.sizeMB)}</td>
                    <td className="hide-sm nowrap">{agoIso(i.created)}</td>
                    <td><div className="row-actions">
                      <button className="btn btn-sm" onClick={dep.blocked ? undefined : dep.run} disabled={!!dep.blocked} title={dep.note}>Desplegar</button>
                      <button className="btn btn-sm btn-quiet" onClick={() => set({ drawer: { type: 'image', id: key } })}>Detalles</button>
                    </div></td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      ) : (
        <Empty icon="search">{imgs.length ? <>Ninguna imagen coincide. <button className="link-btn" onClick={() => set({ imgFilter: 'all', imgSearch: '' })}>Mostrar todas</button></> : 'No hay imágenes en el servidor.'}</Empty>
      )}
      {rows.length > 0 && pg.pager}
    </Panel>
    <DockerSpace blocked={blocked} />
    </>
  );
}

function Actualizaciones({ d }: { d: Derived }) {
  const s = useStore();
  const u = d.s.updates;
  const hist = usePaged(d.s.history, 'historial');
  const blocked = d.serverBlocked;
  const hint = !d.srvOn ? 'Último dato a las ' + d.lastKnown : u.checking ? 'Buscando…' : d.updatesAvail ? (d.updatesAvail === 1 ? '1 disponible' : d.updatesAvail + ' disponibles') : 'Todo al día';
  return (
    <>
      <Panel title="Actualizaciones de imágenes" id="up-h" hint={<span className="hint" style={{ display: 'block' }}>{hint}</span>}
        right={<button className="btn btn-sm" disabled={!!blocked || u.checking} onClick={() => void run('No se pudo buscar', () => adapter().checkUpdates())}><Icon name={u.checking ? 'loader' : 'refresh'} className={u.checking ? 'i spin' : 'i'} />{u.checking ? 'Buscando…' : 'Buscar ahora'}</button>}>
        <p className="small muted" style={{ margin: 0, padding: '0 var(--pad) 6px' }}>
          Nada se actualiza solo. El servidor compara una vez al día cada imagen con su registro, sin descargarla{u.lastCheck ? ' (última vez: ' + agoIso(u.lastCheck).toLowerCase() + ')' : ''}. Detecta versiones nuevas de la misma etiqueta y, en Nextcloud, también la siguiente versión mayor.
        </p>
        {u.items.length ? (
          <div>
            {u.items.map((it) => {
              const jobRunning = !!s.updJobs[it.container] || it.state === 'updating' || it.state === 'reverting';
              const ctrCompose = d.s.docker.containers.find((c) => c.name === it.container)?.compose;
              const majorTag = it.nextMajor ? it.nextMajor.split(':')[1] || it.nextMajor : '';
              const majorAct = it.nextMajor ? act({ label: jobRunning ? 'Actualizando…' : 'Pasar a la ' + majorTag, icon: 'download', kind: it.state === 'major' ? 'primary' : undefined, spin: jobRunning && it.state === 'major', run: () => doUpdate(it.container, it.image, false, it.nextMajor), blocked: blocked || (jobRunning ? 'Hay una acción en curso.' : '') || (it.state === 'available' ? 'Primero aplica la actualización pendiente de tu versión actual.' : '') }) : null;
              const badge = it.state === 'major' ? st('update', 'Versión mayor nueva') : it.state === 'updating' ? st('restarting', 'Actualizando…') : it.state === 'reverting' ? st('restarting', 'Revirtiendo…') : it.state === 'updated' ? st('ok', 'Actualizada') : it.image.startsWith('nextcloud') ? st('warn', 'Versión nueva') : st('update');
              const a = it.state === 'major' ? majorAct! : it.state === 'updated'
                ? act({ label: 'Revertir', icon: 'restart', run: () => doUpdate(it.container, it.image, true), blocked: blocked || (jobRunning ? 'Hay una acción en curso.' : '') || (it.image.startsWith('nextcloud') ? 'Nextcloud no se puede revertir: ya actualizó su base de datos.' : '') })
                : act({ label: jobRunning ? 'Actualizando…' : 'Actualizar', icon: 'download', kind: 'primary', spin: jobRunning, run: () => doUpdate(it.container, it.image, false), blocked: blocked || (jobRunning ? 'Hay una acción en curso.' : '') });
              return (
                <div className="lrow" key={it.container} style={{ flexWrap: 'wrap', alignItems: 'flex-start', paddingTop: 12, paddingBottom: 12 }}>
                  <span className="ico-box"><Icon name="boxes" /></span>
                  <div className="grow" style={{ minWidth: 200 }}>
                    <span className="strong">{it.container}</span><span className="sub mono">{it.image}</span>
                    <span className="sub" style={{ marginTop: 3, color: it.error ? 'var(--err)' : 'var(--ink2)' }}>{it.error || (it.state === 'updated' ? (it.image.startsWith('nextcloud') ? 'Actualizada. Nextcloud no se puede revertir desde el panel; para volver atrás hace falta una copia de seguridad.' : 'Se conserva la imagen anterior para poder revertir durante 7 días, salvo que liberes espacio en Imágenes.') : it.image.startsWith('nextcloud') ? 'Hay una versión nueva de esta etiqueta. Antes de actualizar, haz una copia de seguridad.' : 'Hay una compilación nueva de esta etiqueta en el registro.')}</span>
                  </div>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 12, flexWrap: 'wrap' }}><Badge s={badge} /><ActButton a={a} /></div>
                  {it.nextMajor && it.state !== 'updating' && (
                    <div className="major-box">
                      <span className="strong small">Versión mayor nueva: <span className="mono">{it.nextMajor}</span></span>
                      <span className="sub">Nextcloud solo admite subir de una versión mayor en una, y el panel lo hace con el mismo cuidado: mantenimiento, copia de la base de datos y comprobación.{it.state === 'available' ? ' Antes, aplica la actualización pendiente de tu versión actual.' : ''}</span>
                      {ctrCompose && <span className="sub" style={{ color: 'var(--warn)' }}>Este contenedor viene de Docker Compose (pila {ctrCompose}). Después cambia la etiqueta a <span className="mono">{majorTag}</span> en tu docker-compose.yml: si lo vuelves a levantar con la etiqueta antigua, Nextcloud no arrancará.</span>}
                      {it.state !== 'major' && majorAct && <span style={{ marginTop: 6 }}><ActButton a={majorAct} /></span>}
                    </div>
                  )}
                  {s.updView[it.container] && <UpdProgress job={s.updView[it.container]} />}
                </div>
              );
            })}
          </div>
        ) : (
          <div className="empty"><Icon name="check" style={{ color: 'var(--ok)' }} />{u.lastCheck ? 'Todas las imágenes en uso están al día.' : 'Todavía no se ha comprobado. La primera búsqueda se hace a los 2 minutos de arrancar el servicio.'}</div>
        )}
      </Panel>

      <Panel title="Historial de acciones" id="hist-h" hint="Se guardan las 300 más recientes">
        {d.s.history.length ? (
          <div className="tbox">
            <table className="tbl">
              <thead><tr><th>Cuándo</th><th>Elemento</th><th>Acción</th><th>Resultado</th><th className="hide-sm">Detalle</th></tr></thead>
              <tbody>
                {hist.rows.map((h, i) => (
                  <tr key={i}><td className="nowrap">{agoIso(h.at)}</td><td className="mono">{h.target}</td><td>{h.action}<span className="sub">{h.user}</span></td><td><Badge s={st(h.result === 'ok' ? 'resOk' : 'resErr')} /></td><td className="hide-sm small muted">{h.detail}</td></tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : <Empty icon="clock">Aún no hay acciones. Aquí aparecerán los despliegues, actualizaciones, descargas y cambios hechos desde el panel.</Empty>}
        {d.s.history.length > 0 && hist.pager}
      </Panel>
    </>
  );
}


function UpdProgress({ job }: { job: import('../../data/types').Job }) {
  return (
    <div className="cklist" style={{ flex: '1 1 100%', marginLeft: 46 }} aria-live="polite">
      {job.steps.map((p, i) => {
        const b = p.state === 'done' ? st('resOk', 'Hecho') : p.state === 'running' ? st('starting', 'En curso') : p.state === 'error' ? st('resErr', 'Error') : st('noCheck', 'Pendiente');
        return (
          <div className="ck" key={i}>
            <Badge s={b} iconOnly />
            <div style={{ flex: 1, minWidth: 0 }}><span className="strong" style={{ display: 'block' }}>{p.label}</span><span className="sub">{p.state === 'running' && job.message ? job.message : b.label}</span></div>
          </div>
        );
      })}
    </div>
  );
}
