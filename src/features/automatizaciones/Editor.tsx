import { useEffect, useRef, useState } from 'react';
import type { Condition, HookURLs, Rule, Step, Trigger } from '../../data/types';
import type { Derived } from '../../state/derive';
import { accept, adapter, nav } from '../../state/store';
import { Icon, Seg } from '../../design/ui';
import { CONDITIONS, DAY_NAMES, DAY_SHORT, STEPS, TRIGGERS, WEEK, sentence } from './rules';

function Days({ value, onChange, label }: { value: number[]; onChange: (v: number[]) => void; label: string }) {
  return (
    <div className="chips" role="group" aria-label={label}>
      {WEEK.map((x) => { const on = value.includes(x); return <button type="button" key={x} className={'chip' + (on ? ' on' : '')} aria-pressed={on} aria-label={DAY_NAMES[x]} onClick={() => onChange(on ? value.filter((y) => y !== x) : [...value, x])}>{DAY_SHORT[x]}</button>; })}
    </div>
  );
}

function Num({ value, onChange, min, max, label, unit, width = 76 }: { value: number | undefined; onChange: (v: number) => void; min: number; max: number; label: string; unit?: string; width?: number }) {
  return (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
      <input className="inp" type="number" inputMode="numeric" aria-label={label} min={min} max={max} value={value ?? ''} style={{ width, height: 34 }}
        onChange={(e) => onChange(e.target.value === '' ? 0 : Math.max(min, Math.min(max, Number(e.target.value))))} />
      {unit && <span className="small muted">{unit}</span>}
    </span>
  );
}

function Select({ value, onChange, label, options, placeholder }: { value: string | undefined; onChange: (v: string) => void; label: string; options: [string, string][]; placeholder: string }) {
  return (
    <select className="inp" aria-label={label} value={value || ''} onChange={(e) => onChange(e.target.value)} style={{ height: 34, width: 'auto', minWidth: 200, maxWidth: '100%', paddingRight: 28 }}>
      <option value="" disabled>{placeholder}</option>
      {options.map(([v, l]) => <option key={v} value={v}>{l}</option>)}
    </select>
  );
}

export function Editor({ d, initial, onClose, onHook }: { d: Derived; initial: Rule; onClose: () => void; onHook: (name: string, h: HookURLs) => void }) {
  const [r, setR] = useState<Rule>(initial);
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  const s = d.s;
  const steps = STEPS.filter((x) => (x.kind !== 'backup' && x.kind !== 'copy-backups' && x.kind !== 'nc-full') || d.hasNC);
  const triggers = TRIGGERS.filter((x) => x.kind !== 'app' || d.hasNC);
  const containers: [string, string][] = s.docker.containers.map((c) => [c.name, c.name]);
  const devices: [string, string][] = s.network.devices.filter((v) => v.ip !== s.network.gateway && v.registered !== 'server')
    .map((v) => [v.mac, (v.name || 'Sin nombre') + ' · ' + v.ip]);
  const t = r.trigger;
  const setT = (p: Partial<Trigger>) => setR({ ...r, trigger: { ...t, ...p } });
  const setC = (i: number, p: Partial<Condition>) => setR({ ...r, conditions: r.conditions.map((c, k) => (k === i ? { ...c, ...p } : c)) });
  const setS = (i: number, p: Partial<Step>) => setR({ ...r, steps: r.steps.map((c, k) => (k === i ? { ...c, ...p } : c)) });
  const move = (i: number, dir: -1 | 1) => { const a = [...r.steps]; const j = i + dir; if (j < 0 || j >= a.length) return; [a[i], a[j]] = [a[j], a[i]]; setR({ ...r, steps: a }); };

  useEffect(() => { ref.current?.querySelector<HTMLElement>('input')?.focus(); }, []);
  useEffect(() => {
    const k = (e: KeyboardEvent) => { if (e.key === 'Escape' && !busy) onClose(); };
    document.addEventListener('keydown', k);
    return () => document.removeEventListener('keydown', k);
  });

  const pickTrigger = (kind: Trigger['kind']) => {
    const def: Record<Trigger['kind'], Trigger> = {
      schedule: { kind, days: [1, 2, 3, 4, 5], time: '07:30' }, hook: { kind },
      container: { kind, container: containers[0]?.[0] || '', event: 'stopped', forMin: 2 }, app: { kind, forMin: 5 },
      metric: { kind, metric: 'temp', above: 85, forMin: 5 }, presence: { kind, mac: '', event: 'arrive' }, pc: { kind, event: 'on' },
      access: { kind, event: 'locked' },
    };
    setR({ ...r, trigger: def[kind] });
  };
  const addCondition = (kind: Condition['kind']) => {
    const def: Record<Condition['kind'], Condition> = { time: { kind, from: '06:00', to: '22:00' }, days: { kind, days: [1, 2, 3, 4, 5] },
      device: { kind, mac: '', present: true }, pc: { kind, pc: 'off' }, container: { kind, container: containers[0]?.[0] || '', running: true } };
    setR({ ...r, conditions: [...r.conditions, def[kind]] });
  };
  const addStep = (kind: Step['kind']) => {
    const def: Record<Step['kind'], Step> = { wake: { kind, waitOn: true }, container: { kind, container: containers[0]?.[0] || '', op: 'restart' }, backup: { kind },
      wait: { kind, minutes: 5 }, scan: { kind }, updates: { kind }, http: { kind, url: 'http://', method: 'POST' }, notify: { kind, text: '', priority: 3 }, 'copy-backups': { kind }, 'nc-full': { kind } };
    setR({ ...r, steps: [...r.steps, def[kind]] });
  };

  const save = async () => {
    setBusy(true); setErr('');
    try {
      const name = r.name.trim() || TRIGGERS.find((x) => x.kind === t.kind)!.label;
      const res = await adapter().saveRule({ ...r, name });
      accept(res.state);
      onClose();
      if (res.hook && (res.hook.local || res.hook.tailscale)) onHook(name, res.hook);
    } catch (e) { setErr(e instanceof Error ? e.message : String(e)); }
    setBusy(false);
  };

  const presenceNote = 'El servidor escanea la red cada ' + s.config.scanMinutes + ' min, así que puede tardar hasta ese tiempo en notarlo.';

  return (
    <>
      <div className="scrim" style={{ zIndex: 50 }} />
      <div className="modal-wrap">
        <div className="wiz glass" role="dialog" aria-modal="true" aria-labelledby="ed-t" ref={ref}>
          <div className="wiz-h">
            <div style={{ minWidth: 0 }}><h2 id="ed-t">{initial.id ? 'Editar automatización' : 'Nueva automatización'}</h2><span className="sub">{sentence(r, s)}</span></div>
            <button className="round" onClick={onClose} disabled={busy} aria-label="Cerrar" title="Cerrar"><Icon name="close" /></button>
          </div>

          <div className="wiz-b" style={{ paddingTop: 10 }}>
            <div className="field"><label className="f-l" htmlFor="au-n">Nombre</label>
              <input id="au-n" className="inp" value={r.name} maxLength={60} placeholder="Por ejemplo, Copia nocturna de Nextcloud" onChange={(e) => setR({ ...r, name: e.target.value })} /></div>

            <div className="field"><span className="f-l">1. Cuándo</span>
              <div className="apps au-kinds" role="radiogroup" aria-label="Cuándo">
                {triggers.map((k) => (
                  <label key={k.kind} className={'opt' + (t.kind === k.kind ? ' on' : '')}>
                    <input type="radio" name="au-trigger" checked={t.kind === k.kind} onChange={() => pickTrigger(k.kind)} />
                    <span className="ico-box"><Icon name={k.icon} /></span>
                    <span style={{ minWidth: 0 }}><span className="strong" style={{ display: 'block' }}>{k.label}</span><span className="sub">{k.hint}</span></span>
                  </label>
                ))}
              </div>
              <div className="au-box">
                {t.kind === 'schedule' && <><Days value={t.days || []} onChange={(v) => setT({ days: v })} label="Días" /><span className="small muted">a las</span>
                  <input className="inp" type="time" aria-label="Hora" value={t.time || ''} onChange={(e) => setT({ time: e.target.value })} style={{ width: 150, height: 34 }} /></>}
                {t.kind === 'hook' && <span className="small muted">Al guardar, el panel te dará un enlace secreto. Pégalo en un atajo del iPhone (o en una automatización NFC de Atajos) y se ejecutará al tocarlo. Funciona en casa y fuera con Tailscale.</span>}
                {t.kind === 'container' && <>
                  <Select label="Servicio" value={t.container} onChange={(v) => setT({ container: v })} options={containers} placeholder="Elige un servicio" />
                  <Seg label="Qué le pasa" value={t.event || 'stopped'} onChange={(v) => setT({ event: v })} options={[['stopped', 'Se detiene'], ['unhealthy', 'Falla'], ['running', 'Arranca']]} />
                  <span className="small muted">durante</span><Num label="Minutos" value={t.forMin} onChange={(v) => setT({ forMin: v })} min={0} max={1440} unit="min" /></>}
                {t.kind === 'app' && <><span className="small muted">Nextcloud no responde durante</span><Num label="Minutos" value={t.forMin} onChange={(v) => setT({ forMin: Math.max(1, v) })} min={1} max={1440} unit="min" />
                  <span className="small muted" style={{ flexBasis: '100%' }}>No salta mientras el panel está actualizando Nextcloud.</span></>}
                {t.kind === 'metric' && <>
                  <Seg label="Qué medir" value={t.metric || 'temp'} onChange={(v) => setT({ metric: v, above: v === 'temp' ? 85 : 90 })} options={[['cpu', 'Procesador'], ['mem', 'Memoria'], ['disk', 'Disco'], ...(s.metrics?.tempC != null ? [['temp', 'Temperatura'] as [string, string]] : [])] as [NonNullable<Trigger['metric']>, string][]} />
                  <span className="small muted">supera</span><Num label="Umbral" value={t.above} onChange={(v) => setT({ above: v })} min={t.metric === 'temp' ? 30 : 1} max={t.metric === 'temp' ? 110 : 100} unit={t.metric === 'temp' ? '°C' : '%'} />
                  <span className="small muted">durante</span><Num label="Minutos" value={t.forMin} onChange={(v) => setT({ forMin: v })} min={0} max={1440} unit="min" />
                  <span className="small muted" style={{ flexBasis: '100%' }}>Solo vuelve a saltar cuando el valor baja {t.metric === 'temp' ? '5 °C' : '5 puntos'} por debajo del umbral.</span></>}
                {t.kind === 'presence' && <>
                  <Select label="Dispositivo" value={t.mac} onChange={(v) => setT({ mac: v })} options={devices} placeholder="Elige un dispositivo" />
                  <Seg label="Qué hace" value={t.event || 'arrive'} onChange={(v) => setT({ event: v })} options={[['arrive', 'Se conecta'], ['leave', 'Se desconecta']]} />
                  <span className="small muted" style={{ flexBasis: '100%' }}>{presenceNote}{t.event === 'leave' ? ' Al irse espera 15 min, porque los móviles apagan el Wi‑Fi a ratos.' : ''}</span></>}
                {t.kind === 'access' && <>
                  <Seg label="Qué acceso" value={t.event || 'locked'} onChange={(v) => setT({ event: v })} options={[['locked', 'Bloqueo por intentos'], ['newip', 'Equipo nuevo'], ['login', 'Cualquier entrada']]} />
                  <span className="small muted" style={{ flexBasis: '100%' }}>{t.event === 'login' ? 'Cada vez que alguien inicia sesión en el panel.' : t.event === 'newip' ? 'La primera vez que alguien entra desde una dirección IP que nunca había usado.' : 'Cuando un equipo falla la contraseña 5 veces seguidas y queda bloqueado 15 minutos.'} Es útil con la acción «Avisar al móvil».</span></>}
                {t.kind === 'pc' && (s.pc.configured
                  ? <Seg label="El PC" value={t.event || 'on'} onChange={(v) => setT({ event: v })} options={[['on', 'Se enciende'], ['off', 'Se apaga']]} />
                  : <span className="small" style={{ color: 'var(--err)' }}>Primero configura el PC en Ajustes › Dispositivos registrados.</span>)}
              </div>
            </div>

            <div className="field"><span className="f-l">2. Solo si <span className="muted" style={{ fontWeight: 400 }}>(opcional)</span></span>
              {r.conditions.map((c, i) => (
                <div className="au-box" key={i}>
                  {c.kind === 'time' && <><span className="small muted">entre las</span><input className="inp" type="time" aria-label="Desde" value={c.from || ''} onChange={(e) => setC(i, { from: e.target.value })} style={{ width: 150, height: 34 }} />
                    <span className="small muted">y las</span><input className="inp" type="time" aria-label="Hasta" value={c.to || ''} onChange={(e) => setC(i, { to: e.target.value })} style={{ width: 150, height: 34 }} /></>}
                  {c.kind === 'days' && <Days value={c.days || []} onChange={(v) => setC(i, { days: v })} label="Días de la condición" />}
                  {c.kind === 'device' && <><Select label="Dispositivo" value={c.mac} onChange={(v) => setC(i, { mac: v })} options={devices} placeholder="Elige un dispositivo" />
                    <Seg label="Está" value={c.present ? 'y' : 'n'} onChange={(v) => setC(i, { present: v === 'y' })} options={[['y', 'Está en casa'], ['n', 'No está']]} /></>}
                  {c.kind === 'pc' && <Seg label="El PC está" value={c.pc || 'off'} onChange={(v) => setC(i, { pc: v })} options={[['on', 'PC encendido'], ['off', 'PC apagado']]} />}
                  {c.kind === 'container' && <><Select label="Servicio" value={c.container} onChange={(v) => setC(i, { container: v })} options={containers} placeholder="Elige un servicio" />
                    <Seg label="Está" value={c.running ? 'y' : 'n'} onChange={(v) => setC(i, { running: v === 'y' })} options={[['y', 'En marcha'], ['n', 'Detenido']]} /></>}
                  <button type="button" className="btn btn-sm btn-quiet btn-icon" style={{ marginLeft: 'auto' }} aria-label="Quitar condición" onClick={() => setR({ ...r, conditions: r.conditions.filter((_, k) => k !== i) })}><Icon name="close" /></button>
                </div>
              ))}
              <div className="chips">{CONDITIONS.map((c) => <button type="button" key={c.kind} className="chip" onClick={() => addCondition(c.kind)}>+ {c.label}</button>)}</div>
            </div>

            <div className="field"><span className="f-l">3. Qué hacer</span>
              {r.steps.map((st, i) => (
                <div className="au-box" key={i}>
                  <span className="au-n">{i + 1}</span>
                  {st.kind === 'wake' && <><span className="strong small">Encender el PC</span>
                    <label className="small" style={{ display: 'flex', gap: 6, alignItems: 'center' }}><input type="checkbox" checked={!!st.waitOn} onChange={(e) => setS(i, { waitOn: e.target.checked })} style={{ accentColor: 'var(--accent)' }} />Esperar a que responda (hasta 3 min)</label></>}
                  {st.kind === 'container' && <><Seg label="Qué hacer" value={st.op || 'restart'} onChange={(v) => setS(i, { op: v })} options={[['start', 'Iniciar'], ['restart', 'Reiniciar'], ['stop', 'Detener']]} />
                    <Select label="Servicio" value={st.container} onChange={(v) => setS(i, { container: v })} options={containers} placeholder="Elige un servicio" /></>}
                  {st.kind === 'backup' && <span className="strong small">Copia de la base de datos de Nextcloud <span className="muted" style={{ fontWeight: 400 }}>· en {s.config.dataRoot}/copias, se guardan las 5 últimas</span></span>}
                  {st.kind === 'wait' && <><span className="strong small">Esperar</span><Num label="Minutos" value={st.minutes} onChange={(v) => setS(i, { minutes: Math.max(1, v) })} min={1} max={120} unit="min" /></>}
                  {st.kind === 'notify' && <>
                    <span className="strong small">Avisar al móvil</span>
                    <Seg label="Prioridad" value={String(st.priority || 3)} onChange={(v) => setS(i, { priority: Number(v) })} options={[['3', 'Normal'], ['4', 'Alta'], ['5', 'Urgente']]} />
                    <input className="inp" aria-label="Mensaje del aviso" value={st.text || ''} maxLength={500} onChange={(e) => setS(i, { text: e.target.value })} placeholder="Mensaje (opcional)" style={{ height: 34, flex: '1 1 260px' }} />
                    {s.notify.configured
                      ? <span className="small muted" style={{ flexBasis: '100%' }}>El aviso lleva el nombre de la automatización como título y el motivo al final.</span>
                      : <span className="small" style={{ flexBasis: '100%', color: 'var(--err)' }}>Los avisos aún no están configurados. <button type="button" className="link-btn" onClick={() => { onClose(); nav('settings', { setTab: 'notify' }); }}>Configurarlos en Ajustes › Avisos</button></span>}</>}
                  {st.kind === 'nc-full' && <><span className="strong small">Copia completa de Nextcloud al disco externo</span>
                    {s.config.backupCopyDir
                      ? <span className="small muted" style={{ flexBasis: '100%' }}>Base de datos y archivos, en {s.config.backupCopyDir}. Solo copia lo nuevo; se guardan las {s.config.ncBackupKeep || 7} últimas.</span>
                      : <span className="small" style={{ flexBasis: '100%', color: 'var(--err)' }}>Falta elegir la carpeta externa. <button type="button" className="link-btn" onClick={() => { onClose(); nav('server'); }}>Elegirla en Servidor › Copias de seguridad</button></span>}</>}
                  {st.kind === 'copy-backups' && <><span className="strong small">Copiar las copias al disco externo</span>
                    {s.config.backupCopyDir
                      ? <span className="small muted" style={{ flexBasis: '100%' }}>A {s.config.backupCopyDir}. Solo copia las que aún no están; allí se guardan las 10 últimas.</span>
                      : <span className="small" style={{ flexBasis: '100%', color: 'var(--err)' }}>Falta elegir la carpeta externa. <button type="button" className="link-btn" onClick={() => { onClose(); nav('server'); }}>Elegirla en Servidor › Copias de seguridad</button></span>}</>}
                  {st.kind === 'scan' && <span className="strong small">Escanear la red</span>}
                  {st.kind === 'updates' && <span className="strong small">Buscar actualizaciones de imágenes</span>}
                  {st.kind === 'http' && <><Seg label="Método" value={st.method || 'POST'} onChange={(v) => setS(i, { method: v })} options={[['POST', 'POST'], ['GET', 'GET']]} />
                    <input className="inp mono" aria-label="Dirección" value={st.url || ''} onChange={(e) => setS(i, { url: e.target.value })} placeholder="http://192.168.1.50:8123/api/webhook/…" style={{ height: 34, flex: '1 1 260px' }} spellCheck={false} />
                    <span className="small muted" style={{ flexBasis: '100%' }}>Solo direcciones de tu red local o de Tailscale.</span></>}
                  <span style={{ marginLeft: 'auto', display: 'inline-flex', gap: 2 }}>
                    <button type="button" className="btn btn-sm btn-quiet btn-icon" aria-label="Subir" disabled={i === 0} onClick={() => move(i, -1)}><Icon name="chev" style={{ transform: 'rotate(-90deg)' }} /></button>
                    <button type="button" className="btn btn-sm btn-quiet btn-icon" aria-label="Bajar" disabled={i === r.steps.length - 1} onClick={() => move(i, 1)}><Icon name="chev" style={{ transform: 'rotate(90deg)' }} /></button>
                    <button type="button" className="btn btn-sm btn-quiet btn-icon" aria-label="Quitar acción" onClick={() => setR({ ...r, steps: r.steps.filter((_, k) => k !== i) })}><Icon name="close" /></button>
                  </span>
                </div>
              ))}
              {!r.steps.length && <span className="f-msg">Añade al menos una acción. Se ejecutan en orden y se detienen si una falla.</span>}
              <div className="chips">{steps.map((x) => <button type="button" key={x.kind} className="chip" onClick={() => addStep(x.kind)} disabled={r.steps.length >= 10}>+ {x.label}</button>)}</div>
            </div>

            <div className="field">
              <label className="small" style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
                <input type="checkbox" checked={!!r.notifyFail} disabled={!s.notify.configured} onChange={(e) => setR({ ...r, notifyFail: e.target.checked })} style={{ accentColor: 'var(--accent)' }} />
                Avisarme al móvil si falla
              </label>
              <span className="f-msg">{s.notify.configured ? 'Te llega un aviso con la acción que falló y el motivo.' : <>Necesita los avisos configurados. <button type="button" className="link-btn" onClick={() => { onClose(); nav('settings', { setTab: 'notify' }); }}>Configurarlos en Ajustes › Avisos</button></>}</span>
            </div>

            {t.kind !== 'schedule' && (
              <div className="field"><span className="f-l">Pausa entre ejecuciones</span>
                <div><Seg label="Pausa" value={String(r.cooldownMin)} onChange={(v) => setR({ ...r, cooldownMin: Number(v) })} options={[['0', 'Ninguna'], ['5', '5 min'], ['15', '15 min'], ['30', '30 min'], ['60', '1 h']]} /></div>
                <span className="f-msg">Evita que se repita en bucle si la situación va y viene.</span></div>
            )}
          </div>

          <div className="wiz-f">
            <span className="note" style={err ? { color: 'var(--err)' } : undefined}>{err || 'Se guarda en el servidor y funciona aunque el panel esté cerrado.'}</span>
            <button className="btn" onClick={onClose} disabled={busy}>Cancelar</button>
            <button className="btn btn-primary" onClick={() => void save()} disabled={busy || !r.steps.length}>{busy ? 'Guardando…' : 'Guardar'}</button>
          </div>
        </div>
      </div>
    </>
  );
}
