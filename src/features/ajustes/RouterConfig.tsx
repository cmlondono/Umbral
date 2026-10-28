import { useState } from 'react';
import type { RouterConfig } from '../../data/types';
import type { Derived } from '../../state/derive';
import { accept, adapter, confirmModal, toast } from '../../state/store';
import { Badge, Icon, Panel, Seg, dur, st } from '../../design/ui';

const GUIDE: Record<'openwrt' | 'mikrotik', { url: string; user: string; steps: string[] }> = {
  openwrt: {
    url: 'https://192.168.1.1', user: 'root',
    steps: [
      'Funciona con OpenWrt 21 o posterior con LuCI, que viene de serie.',
      'Usa el usuario root y su contraseña: es el que tiene permiso para leer los equipos y cambiar el cortafuegos.',
      'Si la conexión falla con «no tiene luci-rpc», instala el paquete luci-mod-rpc en el router (Sistema › Software).',
      'Si LuCI usa un certificado propio, marca «Aceptar el certificado del router».',
    ],
  },
  mikrotik: {
    url: 'https://192.168.88.1', user: 'umbral',
    steps: [
      'Necesita RouterOS 7.1 o posterior, que trae la API REST.',
      'Activa el servicio www-ssl en IP › Services (o www, si prefieres http en tu red).',
      'Crea un usuario propio para Umbral en System › Users, con un grupo que tenga read, write, reboot y rest-api.',
      'Si el router usa su certificado autofirmado, marca «Aceptar el certificado del router».',
    ],
  },
};

/* Integración con el router: solo los que tienen API (OpenWrt y MikroTik).
 * La contraseña se guarda en el servidor y nunca vuelve al navegador. */
export function RouterSettings({ d }: { d: Derived }) {
  const saved = d.s.config.router;
  const rs = d.s.router;
  const [f, setF] = useState<RouterConfig>(saved.kind ? saved : { kind: '', url: '', user: '', insecure: false });
  const [pass, setPass] = useState('');
  const [busy, setBusy] = useState(false);
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null);
  const blocked = d.serverBlocked;
  const kind = f.kind || '';
  const g = kind ? GUIDE[kind] : null;
  const dirty = JSON.stringify(f) !== JSON.stringify(saved) || !!pass;
  const complete = !!kind && /^https?:\/\/\S+/.test(f.url) && !!f.user.trim() && (!!pass || rs.hasPassword);

  const pick = (k: RouterConfig['kind']) => {
    setMsg(null);
    if (!k) { setF({ kind: '', url: '', user: '', insecure: false }); return; }
    setF({ kind: k, url: f.url || GUIDE[k].url, user: f.kind === k ? f.user : GUIDE[k].user, insecure: f.insecure });
  };
  const test = async () => {
    setBusy(true); setMsg(null);
    try { const i = await adapter().testRouter(f, pass || undefined); setMsg({ ok: true, text: 'Conectado con ' + (i.model || 'el router') + (i.firmware ? ' · ' + i.firmware : '') + '. Ya puedes guardar.' }); }
    catch (e) { setMsg({ ok: false, text: e instanceof Error ? e.message : String(e) }); }
    setBusy(false);
  };
  const save = async () => {
    setBusy(true); setMsg(null);
    try {
      accept(await adapter().saveRouter(f, pass || undefined));
      setPass('');
      toast({ tone: 'ok', title: 'Router guardado', body: 'En Red verás sus nombres y podrás bloquear el acceso a Internet de un equipo.' });
    } catch (e) { setMsg({ ok: false, text: e instanceof Error ? e.message : String(e) }); }
    setBusy(false);
  };
  const remove = () => confirmModal({
    title: '¿Quitar la integración con el router?', body: 'Umbral dejará de leerlo y olvidará su contraseña. Los equipos que tengas bloqueados seguirán bloqueados en el router hasta que los desbloquees desde él.',
    confirmLabel: 'Quitar integración', confirmCls: 'btn btn-danger-solid',
    onConfirm: async () => { try { accept(await adapter().saveRouter({ kind: '', url: '', user: '', insecure: false })); setF({ kind: '', url: '', user: '', insecure: false }); toast({ tone: 'ok', title: 'Integración quitada', body: 'La contraseña del router se ha borrado del servidor.' }); } catch (e) { setMsg({ ok: false, text: e instanceof Error ? e.message : String(e) }); } },
  });

  const status = !rs.configured ? st('notConfigured', 'Sin integración') : rs.connected ? st('connected') : st('err', 'Sin respuesta');

  return (
    <Panel title="Router" id="router-h" hint="Solo routers con API: OpenWrt y MikroTik" right={<Badge s={d.srvOn ? status : st('unknown')} />}>
      <div className="pb" style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
        {rs.configured && (rs.connected && rs.info
          ? <p className="f-msg" style={{ margin: 0 }}>Conectado con <b>{rs.info.model}</b> · {rs.info.firmware}{rs.info.uptimeSec ? ' · encendido hace ' + dur(rs.info.uptimeSec) : ''} · {rs.leases} equipos en su DHCP{rs.blocked.length ? ' · ' + rs.blocked.length + ' sin Internet' : ''}.</p>
          : <p className="f-msg bad" style={{ margin: 0 }}>{rs.error}</p>)}
        <div>
          <span className="f-l" style={{ display: 'block', marginBottom: 6 }}>Tipo de router</span>
          <Seg label="Tipo de router" value={kind} onChange={pick} options={[['', 'Sin integración'], ['openwrt', 'OpenWrt'], ['mikrotik', 'MikroTik']]} />
          {!kind && <p className="f-msg">El router de la mayoría de proveedores no tiene una API que Umbral pueda usar. Si cambias a uno con OpenWrt o MikroTik, configúralo aquí y podrás ver sus nombres de equipos, bloquear el acceso a Internet de un dispositivo y reiniciarlo desde el panel.</p>}
        </div>
        {g && (
          <>
            <div className="form-grid">
              <div className="field"><label className="f-l" htmlFor="rt-u">Dirección</label>
                <input id="rt-u" className="inp mono" value={f.url} onChange={(e) => setF({ ...f, url: e.target.value.trim() })} placeholder={g.url} spellCheck={false} autoComplete="off" /></div>
              <div className="field"><label className="f-l" htmlFor="rt-n">Usuario</label>
                <input id="rt-n" className="inp mono" value={f.user} onChange={(e) => setF({ ...f, user: e.target.value.trim() })} placeholder={g.user} spellCheck={false} autoComplete="off" /></div>
              <div className="field"><label className="f-l" htmlFor="rt-p">Contraseña</label>
                <input id="rt-p" className="inp" type="password" value={pass} onChange={(e) => setPass(e.target.value)} placeholder={rs.hasPassword ? 'Guardada. Escribe otra para cambiarla' : ''} autoComplete="new-password" /></div>
            </div>
            <label className="small" style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
              <input type="checkbox" checked={f.insecure} onChange={(e) => setF({ ...f, insecure: e.target.checked })} style={{ accentColor: 'var(--accent)' }} />
              Aceptar el certificado del router aunque sea autofirmado <span className="muted">(habitual en routers de casa)</span>
            </label>
            <ul className="small muted" style={{ margin: 0, paddingLeft: 18 }}>{g.steps.map((x) => <li key={x}>{x}</li>)}</ul>
          </>
        )}
        {msg && <p className={'f-msg' + (msg.ok ? '' : ' bad')} role="status" style={{ margin: 0 }}>{msg.ok && <Icon name="check" style={{ width: 14, height: 14, verticalAlign: '-2px', color: 'var(--ok)' }} />} {msg.text}</p>}
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
          {kind && <button className="btn" onClick={() => void test()} disabled={busy || !!blocked || !complete}>{busy ? 'Probando…' : 'Probar conexión'}</button>}
          {kind && <button className="btn btn-primary" onClick={() => void save()} disabled={busy || !!blocked || !complete || !dirty}>Guardar</button>}
          {rs.configured && <button className="btn btn-quiet btn-danger" onClick={remove} disabled={busy || !!blocked} style={{ marginLeft: 'auto' }}>Quitar integración</button>}
        </div>
      </div>
    </Panel>
  );
}
