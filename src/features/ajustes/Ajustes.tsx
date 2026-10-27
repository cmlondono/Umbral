import { useState } from 'react';
import type { Config } from '../../data/types';
import type { Derived } from '../../state/derive';
import { adapter, logout, nav, set, setPrefs, toast, useStore, type Prefs, type SetTab } from '../../state/store';
import { Badge, Icon, Panel, Seg, dur, st, type StateInfo } from '../../design/ui';
import { usePaged } from '../../design/Pager';
import { toolObj } from '../../state/actions';
import { DobleFactor } from './DobleFactor';

const TABS: [SetTab, string][] = [['ui', 'Interfaz'], ['devices', 'Dispositivos registrados'], ['integrations', 'Integraciones'], ['security', 'Seguridad y acceso'], ['pending', 'Configuración pendiente'], ['about', 'Información del sistema']];

export function Ajustes({ d }: { d: Derived }) {
  const s = useStore();
  const pend = pending(d);
  const left = pend.filter((p) => !p.done).length;
  return (
    <div className="set">
      <nav className="set-nav" aria-label="Secciones de ajustes">
        {TABS.map(([k, l]) => (
          <button key={k} className={'nav-btn' + (s.setTab === k ? ' on' : '')} aria-current={s.setTab === k ? 'true' : undefined} onClick={() => set({ setTab: k })}>
            <span className="nav-lbl">{l}</span>{k === 'pending' && left > 0 && <span className="tag">{left}</span>}
          </button>
        ))}
      </nav>
      <div className="col">
        {s.setTab === 'ui' && <Interfaz />}
        {s.setTab === 'devices' && <Equipos d={d} />}
        {s.setTab === 'integrations' && <Integraciones d={d} />}
        {s.setTab === 'security' && <Seguridad d={d} />}
        {s.setTab === 'pending' && (
          <Panel title="Configuración pendiente" hint={left + ' de ' + pend.length + ' pendientes'}>
            {pend.map((p) => (
              <div className="setrow" key={p.title}>
                <div className="grow" style={{ display: 'flex', gap: 12, alignItems: 'flex-start' }}>
                  <Icon name={p.done ? 'check' : 'circle'} className={'i ' + (p.done ? 'check-done' : 'check-todo')} style={{ marginTop: 1 }} />
                  <span><span className="strong">{p.title}</span><span className="sub">{p.done ? 'Completado.' : p.why}</span></span>
                </div>
                <button className="btn btn-sm" onClick={p.go}>{p.goLabel}</button>
              </div>
            ))}
          </Panel>
        )}
        {s.setTab === 'about' && (
          <Panel title="Información del sistema">
            <dl className="dl" style={{ padding: '6px var(--pad)' }}>
              {[
                ['Aplicación', 'Umbral ' + d.s.panel.version],
                ['Consumo del panel', d.s.panel.memMB + ' MB de memoria, en marcha desde hace ' + dur(d.s.panel.uptimeSec)],
                ['Servidor', d.s.server.hostname + ' · ' + d.s.server.os],
                ['Docker', d.s.docker.available ? 'Versión ' + d.s.docker.version : 'Sin respuesta'],
                ['Datos del panel', d.s.panel.dataDir + ' (archivos JSON, sin base de datos)'],
                ['Preferencias de interfaz', 'Guardadas en este navegador'],
              ].map(([k, v]) => <div key={k}><dt>{k}</dt><dd>{v}</dd></div>)}
            </dl>
          </Panel>
        )}
      </div>
    </div>
  );
}

function Interfaz() {
  const { prefs } = useStore();
  const row = <K extends keyof Prefs>(key: K, title: string, sub: string, opts: [Prefs[K], string][]) => (
    <div className="setrow"><div className="grow"><span className="strong">{title}</span><span className="sub">{sub}</span></div>
      <Seg label={title} value={prefs[key] as string} options={opts as [string, string][]} onChange={(v) => setPrefs({ [key]: v } as Partial<Prefs>)} /></div>
  );
  return (
    <Panel title="Preferencias de interfaz" hint="Se guardan en este navegador">
      {row('theme', 'Tema', 'Apariencia del panel. Automático sigue al sistema.', [['auto', 'Automático'], ['light', 'Claro'], ['dark', 'Oscuro']])}
      {row('wallpaper', 'Fondo', 'El fondo ambiente deja ver el efecto de cristal en la navegación.', [['ambient', 'Ambiente'], ['plain', 'Liso']])}
      {row('transparency', 'Transparencia', 'Reducida sustituye el cristal por superficies opacas: más contraste y menos consumo gráfico.', [['normal', 'Cristal'], ['reduced', 'Reducida']])}
      {row('density', 'Densidad', 'La vista compacta muestra más filas en tablas y listas.', [['comfortable', 'Cómoda'], ['compact', 'Compacta']])}
      {row('autoRefresh', 'Actualización automática', 'Cada cuánto se piden datos nuevos al servidor. Intervalos largos consumen menos recursos.', [['off', 'Manual'], ['30s', '30 s'], ['1m', '1 min'], ['5m', '5 min']])}
    </Panel>
  );
}

/* Formulario de configuración del servidor: se guarda en el servidor, no en el navegador. */
function useConfigForm(d: Derived) {
  const [f, setF] = useState<Config>(d.s.config);
  const [err, setErr] = useState('');
  const [saving, setSaving] = useState(false);
  const dirty = JSON.stringify(f) !== JSON.stringify(d.s.config);
  const save = async () => {
    setSaving(true); setErr('');
    try {
      const snap = await adapter().saveConfig(f);
      set({ snap, lastOk: Date.now(), online: true });
      setF(snap.config);
    } catch (e) { setErr(e instanceof Error ? e.message : String(e)); }
    setSaving(false);
  };
  return { f, setF, err, saving, dirty, save, reset: () => { setF(d.s.config); setErr(''); } };
}

function Field({ id, label, value, onChange, placeholder, msg, mono, inputMode }: { id: string; label: string; value: string; onChange: (v: string) => void; placeholder?: string; msg?: string; mono?: boolean; inputMode?: 'numeric' }) {
  return (
    <div className="field"><label className="f-l" htmlFor={id}>{label}</label>
      <input id={id} className={'inp' + (mono ? ' mono' : '')} value={value} placeholder={placeholder} onChange={(e) => onChange(e.target.value)} spellCheck={false} autoComplete="off" inputMode={inputMode} />
      {msg && <span className="f-msg">{msg}</span>}</div>
  );
}

function SaveBar({ form, d }: { form: ReturnType<typeof useConfigForm>; d: Derived }) {
  return (
    <div className="setrow" style={{ borderTop: '1px solid var(--line)' }}>
      <div className="grow">{form.err ? <span className="f-msg bad">{form.err}</span> : <span className="sub">{form.dirty ? 'Hay cambios sin guardar.' : 'Se guarda en el servidor y se aplica al momento.'}</span>}</div>
      <div style={{ display: 'flex', gap: 8 }}>
        {form.dirty && <button className="btn btn-sm btn-quiet" onClick={form.reset}>Descartar</button>}
        <button className="btn btn-sm btn-primary" disabled={!form.dirty || form.saving || !d.srvOn} onClick={() => void form.save()}>{form.saving ? 'Guardando…' : 'Guardar cambios'}</button>
      </div>
    </div>
  );
}

function Equipos({ d }: { d: Derived }) {
  const form = useConfigForm(d);
  const { f, setF } = form;
  const pc = (p: Partial<Config['pc']>) => setF({ ...f, pc: { ...f.pc, ...p } });
  const candidates = d.s.network.devices.filter((v) => v.registered !== 'server' && v.ip !== d.s.network.gateway);
  return (
    <>
      <Panel title="PC de escritorio" hint="Para encenderlo y comprobar si responde">
        <div className="pb" style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
          {candidates.length > 0 && (
            <div className="field"><span className="f-l">Rellenar con un equipo detectado en la red</span>
              <div className="chips">{candidates.slice(0, 12).map((v) => <button key={v.mac} className={'chip' + (f.pc.mac === v.mac ? ' on' : '')} aria-pressed={f.pc.mac === v.mac} onClick={() => pc({ ip: v.ip, mac: v.mac, name: f.pc.name || v.name })}>{(v.name || 'Sin nombre') + ' · ' + v.ip}</button>)}</div>
              <span className="f-msg">Enciende el PC antes para que aparezca. La MAC debe ser la de la tarjeta de red por cable.</span></div>
          )}
          <div className="fgrid">
            <Field id="pc-n" label="Nombre" value={f.pc.name} onChange={(v) => pc({ name: v })} placeholder="ESCRITORIO-PC" />
            <Field id="pc-ip" label="Dirección IP" value={f.pc.ip} onChange={(v) => pc({ ip: v.trim() })} placeholder="192.168.1.20" msg="Conviene reservarla en el router para que no cambie." />
            <Field id="pc-mac" label="Dirección MAC" value={f.pc.mac} onChange={(v) => pc({ mac: v.trim() })} placeholder="3C:7C:3F:A1:52:0E" mono msg="A esta dirección se envía el paquete de encendido." />
            <Field id="pc-port" label="Puerto de comprobación" value={String(f.pc.checkPort || '')} onChange={(v) => pc({ checkPort: parseInt(v, 10) || 0 })} inputMode="numeric" msg="3389 si usas Escritorio remoto. Si el firewall lo bloquea, el PC aparecerá apagado." />
          </div>
          <details className="det"><summary>Opciones avanzadas</summary>
            <div style={{ marginTop: 12 }}><Field id="pc-bc" label="Dirección de difusión" value={f.pc.broadcast} onChange={(v) => pc({ broadcast: v.trim() })} placeholder="Automática" msg="Déjala vacía salvo que el PC esté en otra subred." /></div>
          </details>
        </div>
        <SaveBar form={form} d={d} />
      </Panel>
      <Panel title="Servidor" hint="Este equipo">
        <div className="pb"><div className="fgrid">
          <Field id="srv-n" label="Nombre visible" value={f.serverName} onChange={(v) => setF({ ...f, serverName: v })} />
          <Field id="srv-ssh" label="Usuario para SSH" value={f.sshUser} onChange={(v) => setF({ ...f, sshUser: v.trim() })} placeholder="tu usuario de Debian" msg={'Para el comando ssh ' + (f.sshUser ? f.sshUser + '@' : '') + d.s.server.ip + '.'} />
        </div></div>
        <SaveBar form={form} d={d} />
      </Panel>
    </>
  );
}

function Integraciones({ d }: { d: Derived }) {
  const form = useConfigForm(d);
  const { f, setF } = form;
  const tool = toolObj(d);
  const x = d.s;
  const rows: { name: string; purpose: string; badge: StateInfo; need: string }[] = [
    { name: 'Docker', purpose: 'Contenedores, imágenes, despliegues y actualizaciones', badge: x.docker.available ? st('connected') : st('err', 'Sin respuesta'), need: x.docker.available ? 'Versión ' + x.docker.version + '.' : x.docker.error || 'Añade el usuario umbral al grupo docker.' },
    { name: 'Métricas del sistema', purpose: 'CPU, memoria, discos y red, leídas de /proc', badge: x.metrics ? st('connected') : st('err', 'Sin datos'), need: x.metrics ? 'Sin agentes extra. Puedes retirar Netdata.' : x.metricsError || '' },
    { name: 'Wake-on-LAN', purpose: 'Encender el PC', badge: x.pc.configured ? st('ok', 'Configurado') : st('pending'), need: x.pc.configured ? 'Activa también Wake-on-LAN en la BIOS y en la tarjeta de red del PC.' : 'Añade la IP y la MAC del PC en Dispositivos registrados.' },
    { name: 'Nextcloud', purpose: 'Estado funcional de la aplicación', badge: !x.nextcloud.configured ? st('pending') : x.nextcloud.reachable ? st('connected') : st('err', 'Sin respuesta'), need: x.nextcloud.configured ? 'Se consulta ' + x.nextcloud.url + '/status.php cada minuto.' : 'Indica abajo su dirección, o publica su puerto para detectarla sola.' },
    { name: 'Tailscale', purpose: 'Acceso desde fuera de casa', badge: x.tailscale.running ? st('connected') : x.tailscale.installed ? st('err', 'Desconectado') : st('notConfigured', 'No instalado'), need: x.tailscale.running ? 'Para publicar servicios con HTTPS: sudo tailscale set --operator=umbral.' : 'Ejecuta tailscale up en el servidor.' },
    { name: 'Escaneo de red', purpose: 'Dispositivos detectados', badge: x.network.lastScan ? st('connected') : st('pending'), need: 'Tabla ARP del servidor, cada ' + x.config.scanMinutes + ' min.' },
    { name: 'Escritorio remoto', purpose: 'Abrir la herramienta elegida', badge: tool ? st('ok', tool.label.replace(' (RDP)', '')) : st('pending'), need: tool ? 'Instálala en el PC y en tus dispositivos.' : 'Elige la herramienta en Acceso remoto.' },
  ];
  const pg = usePaged(rows, 'integraciones');
  return (
    <>
      <Panel title="Integraciones y estado de las conexiones">
        <div className="tbox"><table className="tbl">
          <thead><tr><th>Integración</th><th>Estado</th><th className="hide-sm">Qué falta o cómo funciona</th></tr></thead>
          <tbody>{pg.rows.map((r) => <tr key={r.name}><td><span className="strong">{r.name}</span><span className="sub">{r.purpose}</span></td><td><Badge s={d.srvOn ? r.badge : st('unknown')} /></td><td className="hide-sm small muted">{r.need}</td></tr>)}</tbody>
        </table></div>
        {pg.pager}
      </Panel>
      <Panel title="Ajustes de las integraciones">
        <div className="pb" style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
          <div className="fgrid">
            <Field id="nc-url" label="Dirección de Nextcloud" value={f.nextcloud.url} onChange={(v) => setF({ ...f, nextcloud: { ...f.nextcloud, url: v.trim() } })} placeholder="Automática" msg="Vacía: se deduce del puerto que publica el contenedor." />
            <Field id="nc-c" label="Contenedor de Nextcloud" value={f.nextcloud.container} onChange={(v) => setF({ ...f, nextcloud: { ...f.nextcloud, container: v.trim() } })} placeholder="nextcloud" mono msg="Nombre del contenedor de la aplicación." />
            <Field id="dr" label="Carpeta de datos de los despliegues" value={f.dataRoot} onChange={(v) => setF({ ...f, dataRoot: v.trim() })} mono msg="Cada servicio desplegado guarda aquí sus datos." />
            <Field id="scan" label="Escanear la red cada (minutos)" value={String(f.scanMinutes)} onChange={(v) => setF({ ...f, scanMinutes: parseInt(v, 10) || 0 })} inputMode="numeric" />
          </div>
        </div>
        <SaveBar form={form} d={d} />
      </Panel>
    </>
  );
}

function Seguridad({ d }: { d: Derived }) {
  const { user } = useStore();
  const rows: { k: string; v: string; badge?: StateInfo; btn?: { label: string; run: () => void; cls?: string } }[] = [
    { k: 'Acceso permitido desde', v: 'Red local y Tailscale. El servidor rechaza cualquier otra dirección: el panel no se publica en Internet.', badge: st('ok', 'Restringido') },
    { k: 'Sesión', v: 'Iniciada como ' + user + '. La contraseña se guarda en el servidor como hash PBKDF2; el navegador solo tiene una cookie de sesión.', btn: { label: 'Cerrar sesión', run: () => void logout(), cls: 'btn btn-sm' } },
    { k: 'Intentos fallidos', v: 'Tras 5 contraseñas incorrectas desde un mismo equipo, el acceso se bloquea 15 minutos.', badge: st('ok', 'Activo') },
    { k: 'Usuarios', v: 'Se crean y se borran en el servidor, por SSH: sudo -u umbral umbral usuario crear <nombre> -datos /var/lib/umbral. Así nadie puede crear cuentas desde la red.' },
    { k: 'Contraseñas y claves', v: 'Los secretos de los despliegues se generan y se guardan en el servidor. Este panel nunca los muestra ni los almacena en el navegador.' },
  ];
  return (
    <>
    <CambiarContrasena />
    <DobleFactor />
    <Panel title="Seguridad y acceso">
      {rows.map((r) => (
        <div className="setrow" key={r.k}><div className="grow"><span className="strong">{r.k}</span><span className="sub">{r.v}</span></div>
          {r.badge && <Badge s={d.srvOn ? r.badge : st('unknown')} />}
          {r.btn && <button className={r.btn.cls || 'btn btn-sm btn-setup'} onClick={r.btn.run}>{r.btn.label}</button>}
        </div>
      ))}
    </Panel>
    </>
  );
}

function CambiarContrasena() {
  const [open, setOpen] = useState(false);
  const [cur, setCur] = useState('');
  const [next, setNext] = useState('');
  const [rep, setRep] = useState('');
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  const [touched, setTouched] = useState(false);
  const nextErr = touched && next.length < 10 ? 'Usa al menos 10 caracteres.' : '';
  const repErr = touched && rep !== next ? 'Las dos contraseñas no coinciden.' : '';
  const close = () => { setOpen(false); setCur(''); setNext(''); setRep(''); setErr(''); setTouched(false); };
  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (!cur || next.length < 10 || rep !== next) return;
    setBusy(true); setErr('');
    try {
      await adapter().changePassword(cur, next);
      close();
      toast({ tone: 'ok', title: 'Contraseña cambiada', body: 'Se han cerrado las sesiones de tus otros dispositivos. En este sigues dentro.' });
    } catch (x) { setErr(x instanceof Error ? x.message : String(x)); }
    setBusy(false);
  };
  return (
    <Panel title="Contraseña del panel" hint="Distinta de la de Debian">
      {!open ? (
        <div className="setrow"><div className="grow"><span className="strong">Cambiar la contraseña</span><span className="sub">Pide la contraseña actual. Al cambiarla se cierra la sesión en tus otros dispositivos.</span></div>
          <button className="btn btn-sm" onClick={() => setOpen(true)}><Icon name="lock" />Cambiar contraseña</button></div>
      ) : (
        <form className="pb" onSubmit={submit} noValidate style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
          {err && <div className="banner err" role="alert"><Icon name="x" /><div><span>{err}</span></div></div>}
          <div className="field"><label className="f-l" htmlFor="pw-c">Contraseña actual</label>
            <input id="pw-c" className={'inp' + (touched && !cur ? ' bad' : '')} type="password" autoComplete="current-password" value={cur} onChange={(e) => setCur(e.target.value)} autoFocus />
            {touched && !cur && <span className="f-msg bad">Escribe tu contraseña actual.</span>}</div>
          <div className="fgrid">
            <div className="field"><label className="f-l" htmlFor="pw-n">Contraseña nueva</label>
              <input id="pw-n" className={'inp' + (nextErr ? ' bad' : '')} type="password" autoComplete="new-password" value={next} onChange={(e) => setNext(e.target.value)} />
              <span className={'f-msg' + (nextErr ? ' bad' : '')}>{nextErr || 'Mínimo 10 caracteres. Una frase larga es más segura y fácil de recordar.'}</span></div>
            <div className="field"><label className="f-l" htmlFor="pw-r">Repite la contraseña nueva</label>
              <input id="pw-r" className={'inp' + (repErr ? ' bad' : '')} type="password" autoComplete="new-password" value={rep} onChange={(e) => setRep(e.target.value)} />
              {repErr && <span className="f-msg bad">{repErr}</span>}</div>
          </div>
          <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
            <button type="button" className="btn btn-sm btn-quiet" onClick={close}>Cancelar</button>
            <button type="submit" className="btn btn-sm btn-primary" disabled={busy}>{busy ? 'Guardando…' : 'Guardar contraseña'}</button>
          </div>
        </form>
      )}
    </Panel>
  );
}

function pending(d: Derived) {
  const x = d.s;
  return [
    { title: 'Configurar el PC para encenderlo', why: 'Falta su IP y su MAC. Sin ellas no hay Wake-on-LAN.', done: x.pc.configured, goLabel: 'Ir a Dispositivos registrados', go: () => set({ setTab: 'devices' }) },
    { title: 'Elegir la herramienta de escritorio remoto', why: 'Necesaria para conectarte al PC desde el panel.', done: !!x.config.remoteTool, goLabel: 'Ir a Acceso remoto', go: () => nav('remote') },
    ...(d.netdata ? [{ title: 'Retirar Netdata', why: 'Consume memoria de forma continua y este panel ya muestra las métricas esenciales.', done: false, goLabel: 'Ver Netdata', go: () => { nav('services'); set({ drawer: { type: 'service', id: d.netdata!.name } }); } }] : []),
    { title: 'Conectar Tailscale en el servidor', why: 'Para abrir el panel y encender el PC desde fuera de casa.', done: x.tailscale.running, goLabel: 'Ir a Acceso remoto', go: () => nav('remote') },
  ];
}

