import { useEffect, useState } from 'react';
import type { HookURLs, Rule } from '../../data/types';
import type { Derived } from '../../state/derive';
import { adapter, confirmModal, load, run, toast } from '../../state/store';
import { Badge, Empty, Icon, Panel, Switch, agoIso, st } from '../../design/ui';
import { usePaged } from '../../design/Pager';
import { Editor } from './Editor';
import { blank, sentence, templates, triggerIcon } from './rules';

export function Automatizaciones({ d }: { d: Derived }) {
  const s = d.s;
  const [editing, setEditing] = useState<Rule | null>(null);
  const [hook, setHook] = useState<{ name: string; urls: HookURLs } | null>(null);
  const [showTpl, setShowTpl] = useState(false);
  const rules = s.automations;
  const runs = s.runs || [];
  const pg = usePaged(runs, 'ejecuciones');
  const blocked = d.serverBlocked;
  const running = runs.some((r) => r.result === 'running');

  // Mientras algo se ejecuta, se consulta su avance cada 2 s.
  useEffect(() => {
    if (!running) return;
    const t = setInterval(() => void load(), 2000);
    return () => clearInterval(t);
  }, [running]);

  const toggle = (r: Rule, on: boolean) => void run('No se pudo cambiar', () => adapter().setRuleEnabled(r.id, on), { title: on ? 'Activada' : 'Desactivada', body: r.name + (on ? '. El servidor la ejecuta aunque el panel esté cerrado.' : '.') });
  const remove = (r: Rule) => confirmModal({ title: '¿Eliminar «' + r.name + '»?', body: 'Dejará de ejecutarse. Su historial se conserva.' + (r.hasHook ? ' Su enlace dejará de funcionar.' : ''), confirmLabel: 'Eliminar', confirmCls: 'btn btn-danger-solid',
    onConfirm: () => void run('No se pudo eliminar', () => adapter().deleteRule(r.id), { title: 'Automatización eliminada', body: r.name + '.' }) });
  const runNow = (r: Rule) => void run('No se pudo ejecutar', () => adapter().runRule(r.id), { title: 'En marcha: ' + r.name, body: 'Sigue su avance en el historial de abajo.' });
  const newLink = (r: Rule) => confirmModal({ title: '¿Crear un enlace nuevo?', body: 'El enlace actual dejará de funcionar y tendrás que pegar el nuevo en tu atajo o etiqueta.', confirmLabel: 'Crear enlace nuevo',
    onConfirm: async () => { try { const res = await adapter().newHook(r.id); setHook({ name: r.name, urls: res.hook }); } catch (e) { toast({ tone: 'err', title: 'No se pudo crear', body: e instanceof Error ? e.message : String(e) }); } } });

  const tpls = templates(s);

  return (
    <>
      <Panel title="Automatizaciones" id="au-h" hint={rules.length ? undefined : 'Tareas que el servidor hace solo'}
        right={<div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
          {rules.length > 0 && <button className="btn btn-sm" onClick={() => setShowTpl(!showTpl)}>{showTpl ? 'Ocultar plantillas' : 'Plantillas'}</button>}
          <button className="btn btn-sm btn-primary" onClick={() => setEditing(blank())} disabled={!!blocked} title={blocked || undefined}><Icon name="plus" />Nueva automatización</button>
        </div>}>
        {rules.length > 0 && (
          <div>
            {rules.map((r) => {
              const last = r.lastRun;
              const lastBadge = !last ? null : last.result === 'ok' ? st('resOk', 'Correcta') : last.result === 'skipped' ? st('noCheck', 'Omitida') : st('resErr', 'Error');
              return (
                <div className="lrow" key={r.id} style={{ alignItems: 'flex-start', paddingTop: 14, paddingBottom: 14, flexWrap: 'wrap' }}>
                  <span className="ico-box"><Icon name={triggerIcon(r.trigger)} /></span>
                  <div className="grow" style={{ minWidth: 220 }}>
                    <span className="strong">{r.name}</span>
                    <span className="sub" style={{ color: 'var(--ink2)' }}>{sentence(r, s)}</span>
                    <span className="sub" style={{ marginTop: 4, display: 'flex', gap: 10, alignItems: 'center', flexWrap: 'wrap' }}>
                      {last ? <>Última vez {agoIso(last.at).toLowerCase()} {lastBadge && <Badge s={lastBadge} />}</> : 'Aún no se ha ejecutado'}
                      {r.enabled && r.armed === 'done' && <span className="tag">Esperando a que se normalice</span>}
                    </span>
                    {last?.result === 'error' && <span className="sub" style={{ color: 'var(--err)', marginTop: 2 }}>{last.detail}</span>}
                    <span style={{ display: 'flex', gap: 6, marginTop: 8, flexWrap: 'wrap' }}>
                      <button className="btn btn-sm" onClick={() => setEditing(r)} disabled={!!blocked}>Editar</button>
                      <button className="btn btn-sm" onClick={() => runNow(r)} disabled={!!blocked} title="Ejecuta sus acciones ahora, sin esperar al disparador">Ejecutar ahora</button>
                      {r.trigger.kind === 'hook' && <button className="btn btn-sm btn-quiet" onClick={() => newLink(r)} disabled={!!blocked}>Nuevo enlace</button>}
                      <button className="btn btn-sm btn-quiet btn-danger" onClick={() => remove(r)} disabled={!!blocked}>Eliminar</button>
                    </span>
                  </div>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
                    <Badge s={st(r.enabled ? 'active' : 'disabled')} />
                    <Switch on={r.enabled} label={(r.enabled ? 'Desactivar ' : 'Activar ') + r.name} onChange={(v) => toggle(r, v)} disabled={!!blocked} />
                  </div>
                </div>
              );
            })}
          </div>
        )}
        {(rules.length === 0 || showTpl) && (
          <div className="pb" style={{ borderTop: rules.length ? '1px solid var(--line)' : undefined }}>
            <p className="f-msg" style={{ margin: '0 0 12px' }}>{rules.length ? 'Plantillas listas para usar. Se abren en el editor para que las ajustes.' : 'Todavía no hay ninguna. Empieza con una plantilla o crea la tuya con «Nueva automatización».'}</p>
            <div className="apps">
              {tpls.map((t) => (
                <button key={t.id} className="opt au-tpl" onClick={() => { setShowTpl(false); setEditing({ ...t.rule }); }} disabled={!!blocked}>
                  <span className="ico-box"><Icon name={t.icon} /></span>
                  <span style={{ minWidth: 0, textAlign: 'left' }}><span className="strong" style={{ display: 'block' }}>{t.title}</span><span className="sub">{t.desc}</span>
                    {t.needs && <span className="sub" style={{ color: 'var(--warn)', marginTop: 2 }}>{t.needs}</span>}</span>
                </button>
              ))}
            </div>
          </div>
        )}
      </Panel>

      <Panel title="Historial" id="runs-h" hint="Se guardan las 100 más recientes">
        {runs.length ? (
          <div className="tbox">
            <table className="tbl">
              <thead><tr><th>Cuándo</th><th>Automatización</th><th>Resultado</th><th className="hide-sm">Detalle</th></tr></thead>
              <tbody>
                {pg.rows.map((x) => {
                  const b = x.result === 'running' ? st('starting', 'En curso') : x.result === 'ok' ? st('resOk') : x.result === 'skipped' ? st('noCheck', 'Omitida') : st('resErr');
                  return (
                    <tr key={x.id}>
                      <td className="nowrap">{agoIso(x.at)}</td>
                      <td><span className="strong">{x.name}</span><span className="sub">{x.cause}</span></td>
                      <td><Badge s={b} /></td>
                      <td className="hide-sm small">
                        {x.steps?.length ? (
                          <details className="det"><summary>{x.detail || (x.steps.filter((p) => p.result === 'ok').length + ' de ' + x.steps.length + ' acciones')}</summary>
                            <ol style={{ margin: '6px 0 0', paddingLeft: 18 }}>
                              {x.steps.map((p, i) => <li key={i} style={{ color: p.result === 'error' ? 'var(--err)' : 'var(--ink2)' }}>{p.label}: {p.result === 'pending' ? 'pendiente' : p.result === 'running' ? 'en curso…' : p.detail || 'hecho'}{p.ms > 1500 ? ' (' + Math.round(p.ms / 1000) + ' s)' : ''}</li>)}
                            </ol>
                          </details>
                        ) : <span className="muted">{x.detail}</span>}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        ) : <Empty icon="clock">Aquí aparecerá cada ejecución, con el motivo y el resultado de cada acción.</Empty>}
        {runs.length > 0 && pg.pager}
      </Panel>

      {editing && <Editor d={d} initial={editing} onClose={() => setEditing(null)} onHook={(name, urls) => setHook({ name, urls })} />}
      {hook && <HookDialog name={hook.name} urls={hook.urls} onClose={() => setHook(null)} />}
    </>
  );
}

function HookDialog({ name, urls, onClose }: { name: string; urls: HookURLs; onClose: () => void }) {
  const copy = async (u: string) => {
    try { await navigator.clipboard.writeText(u); toast({ tone: 'ok', title: 'Enlace copiado', body: 'Pégalo en tu atajo.' }); }
    catch { toast({ tone: 'info', title: 'Cópialo a mano', body: 'El navegador no permite copiar sin HTTPS: selecciona el enlace y cópialo.' }); }
  };
  return (
    <>
      <div className="scrim" style={{ zIndex: 50 }} />
      <div className="modal-wrap">
        <div className="modal glass" role="dialog" aria-modal="true" aria-labelledby="hk-t" style={{ width: 'min(560px,100%)' }}>
          <h2 id="hk-t">Enlace de «{name}»</h2>
          <p>Guárdalo ahora: por seguridad no se vuelve a mostrar. Si lo pierdes, crea uno nuevo desde la automatización.</p>
          {[['En casa', urls.local], ['Desde fuera, con Tailscale', urls.tailscale]].filter(([, u]) => u).map(([l, u]) => (
            <div className="field" key={l} style={{ marginTop: 14 }}>
              <span className="f-l">{l}</span>
              <div style={{ display: 'flex', gap: 8 }}>
                <input className="inp mono" readOnly value={u} onFocus={(e) => e.target.select()} aria-label={l} />
                <button className="btn" onClick={() => void copy(u!)}><Icon name="copy" />Copiar</button>
              </div>
            </div>
          ))}
          <div className="kv" style={{ marginTop: 16, padding: '4px 14px' }}>
            <div><span className="strong">En el iPhone</span><span /></div>
            <div><span className="small" style={{ textAlign: 'left' }}>1. Abre Atajos y crea un atajo nuevo.<br />2. Añade la acción «Obtener contenido de URL» y pega el enlace. En «Método», elige POST.<br />3. Para usarlo con una etiqueta NFC: pestaña Automatización › Nueva › NFC, escanea la etiqueta y elige ese atajo.</span></div>
          </div>
          <div className="modal-f"><button className="btn btn-primary" onClick={onClose}>Hecho</button></div>
        </div>
      </div>
    </>
  );
}
