import { useEffect, useMemo, useRef, useState } from 'react';
import type { ContainerDetails, LogLine } from '../../data/types';
import { adapter, confirmModal, handleErr, run, set, toast, useStore } from '../../state/store';
import { Icon, Seg, Switch, memLabel } from '../../design/ui';
import { useDialog } from '../../design/useDialog';

/* Secciones extra del panel de detalles de un contenedor: memoria,
 * almacenamiento y acceso al registro. Se leen al abrirlo. */
export function ContainerExtra({ name, memMB, blocked }: { name: string; memMB: number | null; blocked: string }) {
  const [det, setDet] = useState<ContainerDetails | null>(null);
  const [err, setErr] = useState('');
  const [mb, setMb] = useState('');
  const snapAt = useStore().snap?.at;

  useEffect(() => {
    let off = false;
    adapter().details(name).then((d) => { if (!off) { setDet(d); setErr(''); setMb(d.memLimitMB ? String(d.memLimitMB) : ''); } })
      .catch((e) => { if (!off) setErr(e instanceof Error ? e.message : String(e)); });
    return () => { off = true; };
  }, [name, snapAt]);

  const n = Number(mb);
  const valid = Number.isInteger(n) && n >= 32;
  const tooLow = valid && memMB != null && n < memMB * 1.2;
  const apply = () => confirmModal({
    title: '¿Limitar ' + name + ' a ' + memLabel(n) + '?',
    body: 'Si intenta usar más, Docker lo cerrará y lo volverá a arrancar si tiene reinicio automático. Se aplica al momento, sin reiniciarlo. Si lo recreas con Docker Compose, manda el valor de su archivo.',
    confirmLabel: 'Aplicar límite',
    onConfirm: () => void run('No se pudo cambiar el límite', () => adapter().setMemory(name, n), { title: 'Límite aplicado', body: name + ': como mucho ' + memLabel(n) + '.' }),
  });
  const remove = () => confirmModal({
    title: '¿Quitar el límite de memoria de ' + name + '?',
    body: 'Docker solo permite quitarlo recreando el contenedor con la misma imagen y los mismos volúmenes. Estará parado unos segundos; si no arranca, se vuelve al anterior.',
    confirmLabel: 'Quitar límite', confirmCls: 'btn btn-danger-solid',
    onConfirm: () => void run('No se pudo quitar el límite', () => adapter().setMemory(name, 0), { title: 'Sin límite de memoria', body: name + ' puede usar toda la memoria libre.' }),
  });

  return (
    <>
      <div className="ag">
        <h3>Registro</h3>
        <p>Las últimas líneas que ha escrito el contenedor. Útil cuando algo falla.</p>
        <div className="ag-list"><div className="ag-item">
          <button className="btn" onClick={() => set({ logs: name })}><Icon name="terminal" />Ver registro</button>
        </div></div>
      </div>

      <div className="ag">
        <h3>Memoria</h3>
        {err ? <p style={{ color: 'var(--err)' }}>{err}</p> : !det ? <p>Leyendo…</p> : (
          <>
            <p>{det.memLimitMB ? 'Límite actual: ' + memLabel(det.memLimitMB) + '.' : 'Sin límite: puede usar toda la memoria libre del servidor.'}{memMB != null ? ' Ahora usa ' + memLabel(memMB) + '.' : ''} Un límite evita que un servicio deje sin memoria a los demás.</p>
            <div className="mem-row">
              <label className="f-l" htmlFor="mem-mb">Límite en MB</label>
              <input id="mem-mb" className={'inp' + (mb && !valid ? ' bad' : '')} inputMode="numeric" value={mb} placeholder="por ejemplo, 512" onChange={(e) => setMb(e.target.value.replace(/\D/g, ''))} />
              <button className="btn btn-primary" onClick={apply} disabled={!!blocked || !valid || n === det.memLimitMB} title={blocked || undefined}>{det.memLimitMB ? 'Cambiar límite' : 'Aplicar límite'}</button>
              {det.memLimitMB > 0 && <button className="btn btn-quiet btn-danger" onClick={remove} disabled={!!blocked} title={blocked || undefined}>Quitar límite</button>}
            </div>
            <span className={'f-msg' + (mb && (!valid || tooLow) ? ' bad' : '')}>{mb && !valid ? 'Mínimo 32 MB.' : tooLow ? 'Es poco para lo que usa ahora: el servidor rechazará un límite por debajo de ' + Math.ceil(memMB! * 1.2) + ' MB.' : 'Pon al menos un 20 % más de lo que usa en su momento de más carga.'}</span>
          </>
        )}
      </div>

      {det?.mounts && det.mounts.length > 0 && (
        <div className="ag">
          <h3>Datos</h3>
          <p>Dónde guarda su información. Se conservan al actualizar, recrear o eliminar el contenedor.</p>
          <dl className="dl" style={{ margin: 0, padding: 0 }}>
            {det.mounts.map((m) => <div key={m.destination}><dt className="mono">{m.destination}</dt><dd className="mono small">{m.type === 'volume' ? 'Volumen ' + (m.name.length > 24 ? m.name.slice(0, 12) + '…' : m.name) : m.source}</dd></div>)}
          </dl>
        </div>
      )}
    </>
  );
}

const LINES = [['200', '200'], ['500', '500'], ['2000', '2000']] as [string, string][];

export function LogsModal({ name }: { name: string }) {
  const close = () => set({ logs: null });
  const ref = useDialog(true, close);
  const [lines, setLines] = useState<LogLine[] | null>(null);
  const [at, setAt] = useState('');
  const [count, setCount] = useState('200');
  const [q, setQ] = useState('');
  const [onlyErr, setOnlyErr] = useState(false);
  const [busy, setBusy] = useState(false);
  const box = useRef<HTMLDivElement>(null);

  const load = async () => {
    setBusy(true);
    try { const r = await adapter().logs(name, Number(count)); setLines(r.lines); setAt(r.at); }
    catch (e) { if (!handleErr(e, 'No se pudo leer el registro')) setLines((l) => l || []); }
    setBusy(false);
  };
  useEffect(() => { void load(); }, [name, count]); // eslint-disable-line react-hooks/exhaustive-deps
  useEffect(() => { const b = box.current; if (b) b.scrollTop = b.scrollHeight; }, [lines]);

  const shown = useMemo(() => {
    const t = q.trim().toLowerCase();
    return (lines || []).filter((l) => (!onlyErr || l.err) && (!t || l.text.toLowerCase().includes(t)));
  }, [lines, q, onlyErr]);
  const errs = (lines || []).filter((l) => l.err).length;
  const time = (iso: string) => (iso ? new Date(iso).toLocaleString('es', { day: '2-digit', month: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit' }) : '');
  const copy = async () => {
    try { await navigator.clipboard.writeText(shown.map((l) => (l.at ? time(l.at) + ' ' : '') + l.text).join('\n')); toast({ tone: 'ok', title: 'Registro copiado', body: shown.length + ' líneas.' }); }
    catch { toast({ tone: 'info', title: 'Cópialo a mano', body: 'El navegador no permite copiar sin HTTPS: selecciona el texto y cópialo.' }); }
  };

  return (
    <>
      <div className="scrim" style={{ zIndex: 50 }} onClick={close} />
      <div className="modal-wrap">
        <div className="modal glass logs-modal" role="dialog" aria-modal="true" aria-labelledby="lg-t" ref={ref}>
          <div className="logs-h">
            <div>
              <h2 id="lg-t">Registro de {name}</h2>
              <span className="sub">{lines ? shown.length + ' de ' + lines.length + ' líneas' + (errs ? ' · ' + errs + ' por la salida de errores' : '') + (at ? ' · leído a las ' + new Date(at).toLocaleTimeString('es') : '') : 'Leyendo…'}</span>
            </div>
            <button className="btn btn-quiet btn-icon" onClick={close} aria-label="Cerrar registro"><Icon name="close" /></button>
          </div>
          <div className="logs-tools">
            <input className="inp" placeholder="Filtrar líneas" value={q} onChange={(e) => setQ(e.target.value)} aria-label="Filtrar líneas" />
            <span className="logs-sw"><Switch on={onlyErr} onChange={setOnlyErr} label="Solo la salida de errores" /><span className="small">Solo salida de errores</span></span>
            <span className="logs-sw"><span className="small muted">Últimas</span><Seg label="Número de líneas" value={count} options={LINES} onChange={setCount} /></span>
            <button className="btn btn-sm" onClick={() => void load()} disabled={busy}><Icon name="refresh" />{busy ? 'Leyendo…' : 'Actualizar'}</button>
            <button className="btn btn-sm btn-quiet" onClick={() => void copy()} disabled={!shown.length}><Icon name="copy" />Copiar</button>
          </div>
          <div className="logs-box" ref={box} tabIndex={0} aria-label="Líneas del registro">
            {lines && !shown.length && <p className="muted small" style={{ padding: 12 }}>{lines.length ? 'Ninguna línea coincide.' : 'El contenedor no ha escrito nada.'}</p>}
            {shown.map((l, i) => (
              <div key={i} className={'logs-l' + (l.err ? ' err' : '')}>
                <span className="logs-t">{time(l.at)}</span>
                {l.err && <span className="logs-tag" title="Salida de errores">stderr</span>}
                <span className="logs-x">{l.text}</span>
              </div>
            ))}
          </div>
        </div>
      </div>
    </>
  );
}
