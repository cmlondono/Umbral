import { useState } from 'react';
import type { Derived } from '../../state/derive';
import { accept, adapter, confirmModal, handleErr, nav, toast } from '../../state/store';
import { Badge, Icon, Panel, st } from '../../design/ui';

/* Ajustes › Avisos: canal ntfy para la acción «Avisar al móvil» de las
 * automatizaciones. El token se escribe, pero nunca se vuelve a mostrar. */
export function Avisos({ d }: { d: Derived }) {
  const n = d.s.notify;
  const [server, setServer] = useState(n.server || 'https://ntfy.sh');
  const [topic, setTopic] = useState(n.topic);
  const [token, setToken] = useState('');
  const [clearToken, setClearToken] = useState(false);
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  const blocked = d.serverBlocked;
  const users = d.s.automations.filter((r) => r.steps.some((x) => x.kind === 'notify')).length;
  const dirty = server !== (n.server || 'https://ntfy.sh') || topic !== n.topic || !!token || clearToken;
  const publicServer = /^https:\/\/ntfy\.sh\/?$/.test(server.trim());

  const save = async (t = topic) => {
    setBusy(true); setErr('');
    try {
      accept(await adapter().saveNotify(server, t, clearToken ? '' : token || undefined));
      setToken(''); setClearToken(false);
      toast({ tone: 'ok', title: t ? 'Avisos guardados' : 'Avisos desactivados', body: t ? 'Envía un aviso de prueba para comprobarlo.' : 'Las automatizaciones con «Avisar al móvil» fallarán hasta que los configures.' });
    } catch (e) { setErr(e instanceof Error ? e.message : String(e)); }
    setBusy(false);
  };
  const generate = async () => {
    try { setTopic((await adapter().newTopic()).topic); } catch (e) { handleErr(e, 'No se pudo generar'); }
  };
  const test = async () => {
    setBusy(true);
    try { await adapter().testNotify(); toast({ tone: 'ok', title: 'Aviso de prueba enviado', body: 'Debería llegarte al móvil en unos segundos.' }); }
    catch (e) { handleErr(e, 'No se pudo enviar'); }
    setBusy(false);
  };
  const disable = () => confirmModal({
    title: '¿Desactivar los avisos?', body: users ? users + (users === 1 ? ' automatización usa' : ' automatizaciones usan') + ' «Avisar al móvil» y fallarán en ese paso.' : 'Ninguna automatización los usa ahora.',
    confirmLabel: 'Desactivar', confirmCls: 'btn btn-danger-solid', onConfirm: () => { setTopic(''); return save(''); },
  });

  return (
    <>
      <Panel title="Avisos al móvil" hint="Con ntfy, gratis y sin crear cuentas"
        right={<Badge s={n.configured ? st('ok', 'Configurados') : st('noCheck', 'Sin configurar')} />}>
        <div className="pb">
          <p className="f-msg" style={{ margin: '0 0 14px' }}>Las automatizaciones pueden enviarte un aviso al móvil con la acción «Avisar al móvil». Umbral los envía a través de <b>ntfy</b>: el servidor público ntfy.sh o uno propio.</p>
          <div className="form-grid">
            <div className="field"><label className="f-l" htmlFor="nt-s">Servidor</label>
              <input id="nt-s" className="inp mono" value={server} onChange={(e) => setServer(e.target.value)} placeholder="https://ntfy.sh" spellCheck={false} autoComplete="off" />
              <span className="f-msg">{publicServer ? 'El servidor público. Para uno propio, despliega ntfy desde Servicios y pon aquí su dirección.' : 'Tu servidor ntfy. En iPhone, para recibir avisos al instante, tu servidor necesita la opción upstream-base-url: https://ntfy.sh.'}</span></div>
            <div className="field"><label className="f-l" htmlFor="nt-t">Tema</label>
              <div style={{ display: 'flex', gap: 8 }}>
                <input id="nt-t" className="inp mono" value={topic} onChange={(e) => setTopic(e.target.value)} placeholder="umbral-…" spellCheck={false} autoComplete="off" />
                <button className="btn" onClick={() => void generate()} disabled={!!blocked}>Generar</button>
              </div>
              <span className="f-msg">{publicServer ? 'En ntfy.sh, quien conozca el tema puede leer tus avisos: usa uno generado y no lo compartas.' : 'El canal donde se publican los avisos.'}</span></div>
            <div className="field"><label className="f-l" htmlFor="nt-k">Token de acceso <span className="muted" style={{ fontWeight: 400 }}>(opcional)</span></label>
              <input id="nt-k" className="inp mono" type="password" value={token} onChange={(e) => { setToken(e.target.value); setClearToken(false); }} placeholder={n.hasToken ? 'Guardado. Escribe otro para cambiarlo' : 'tk_…'} autoComplete="new-password" />
              <span className="f-msg">Solo si tu servidor ntfy exige usuario. Se guarda en el servidor y no se vuelve a mostrar.
                {n.hasToken && !token && <> <label style={{ display: 'inline-flex', gap: 6, alignItems: 'center' }}><input type="checkbox" checked={clearToken} onChange={(e) => setClearToken(e.target.checked)} style={{ accentColor: 'var(--accent)' }} />Borrar el token guardado</label></>}</span></div>
          </div>
          {err && <p className="f-msg bad" role="alert">{err}</p>}
          <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', marginTop: 14 }}>
            <button className="btn btn-primary" onClick={() => void save()} disabled={busy || !!blocked || !topic.trim() || !dirty} title={blocked || undefined}>{busy ? 'Guardando…' : 'Guardar'}</button>
            {n.configured && <button className="btn" onClick={() => void test()} disabled={busy || !!blocked || dirty} title={dirty ? 'Guarda los cambios antes de probar' : undefined}><Icon name="bell" />Enviar aviso de prueba</button>}
            {n.configured && <button className="btn btn-quiet btn-danger" onClick={disable} disabled={busy || !!blocked} style={{ marginLeft: 'auto' }}>Desactivar avisos</button>}
          </div>
        </div>
      </Panel>

      <Panel title="Recibirlos en el móvil">
        <ol className="steps-list">
          <li><span className="strong">Instala la app ntfy</span><span className="sub">Gratis en la App Store y en Google Play.</span></li>
          <li><span className="strong">Suscríbete al tema</span><span className="sub">Pulsa + y escribe el tema{n.topic ? <> <code className="mono">{n.topic}</code></> : ''}.{!publicServer && n.configured ? <> Elige «Usar otro servidor» y escribe <code className="mono">{n.server}</code>.</> : ''}</span></li>
          <li><span className="strong">Prueba</span><span className="sub">Usa «Enviar aviso de prueba» y comprueba que te llega.</span></li>
          <li><span className="strong">Úsalos en tus automatizaciones</span><span className="sub">Añade la acción «Avisar al móvil», o empieza con una plantilla como «Avisarme si un servicio se cae». {users ? 'Ahora lo usan ' + users + '.' : ''} <button className="link-btn" onClick={() => nav('auto')}>Ir a Automatizaciones</button></span></li>
        </ol>
      </Panel>
    </>
  );
}
