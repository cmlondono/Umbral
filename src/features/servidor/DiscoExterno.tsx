import { useEffect, useState } from 'react';
import type { Backups, ExtDisk, Job } from '../../data/types';
import type { Derived } from '../../state/derive';
import { accept, adapter, follow, handleErr, toast, useStore } from '../../state/store';
import { Badge, Banner, Empty, Icon, agoIso, st, type StateInfo } from '../../design/ui';
import { Codigo2FA, Falta2FA } from './Codigo2FA';

/* Disco externo de las copias: Umbral detecta los discos USB conectados y,
 * con un botón, los prepara (formatear en ext4 si hace falta, montarlos en
 * cada arranque y crear su carpeta). Lo hace el ayudante de root del servidor. */

const gb = (b: number) => (b >= 1e12 ? (b / 1e12).toFixed(1).replace('.0', '') + ' TB' : Math.round(b / 1e9) + ' GB');
const FS: Record<string, string> = { ntfs: 'NTFS', exfat: 'exFAT', vfat: 'FAT32', ext4: 'ext4', ext3: 'ext3', ext2: 'ext2', btrfs: 'Btrfs', xfs: 'XFS', hfsplus: 'HFS+', apfs: 'APFS' };

function content(dk: ExtDisk) {
  if (!dk.parts.length) return 'Vacío, sin particiones';
  return dk.parts.map((p) => (FS[p.fsType] || p.fsType || 'Formato desconocido') + (p.label ? ' «' + p.label + '»' : '') + ', ' + gb(p.sizeBytes) + (p.mount ? ', montado en ' + p.mount : '')).join(' · ');
}

function badge(dk: ExtDisk): StateInfo {
  switch (dk.state) {
    case 'ready': return st('ok', 'Disco de las copias');
    case 'mounted': case 'ext4': return st('detected', dk.known ? 'Desmontado' : 'Se puede usar');
    case 'other': return st('pending', 'Hay que prepararlo');
    default: return st('na', 'No disponible');
  }
}

type Ask = { disk: ExtDisk; action: 'usar' | 'formatear' };

export function DiscoExterno({ d, b, onDone }: { d: Derived; b: Backups | null; onDone: () => void }) {
  const totp = useStore().totp;
  const cfg = d.s.config;
  const blocked = d.serverBlocked;
  const [ask, setAsk] = useState<Ask | null>(null);
  const [typed, setTyped] = useState('');
  const [job, setJob] = useState<Job | null>(null);
  const [dir, setDir] = useState(cfg.backupCopyDir);
  const [dirErr, setDirErr] = useState('');
  const [busy, setBusy] = useState(false);
  useEffect(() => setDir(cfg.backupCopyDir), [cfg.backupCopyDir]);

  const disks = b?.disks || [];
  const cd = b?.copyDir;
  const running = job?.state === 'running';
  const ready = disks.some((x) => x.state === 'ready');

  // Mientras no haya disco de copias, busca discos nuevos cada 5 s.
  useEffect(() => {
    if (ready || running || blocked) return;
    const t = setInterval(onDone, 5000);
    return () => clearInterval(t);
  }, [ready, running, blocked, onDone]);

  const why = blocked || (b && !b.diskHelper ? 'Falta el ayudante de discos en el servidor: vuelve a ejecutar instalar.sh con esta versión de Umbral.' : '') || (running ? 'Espera a que termine la preparación en curso.' : '');
  const canAct = !why && totp;

  const prepare = async (code: string) => {
    if (!ask) return;
    const j = await adapter().prepareDisk({ action: ask.action, disk: ask.disk.name, serial: ask.disk.serial, size: ask.disk.sizeBytes, code });
    setAsk(null); setTyped(''); setJob(j);
    void (async () => {
      const end = await follow(j.id, setJob);
      if (end?.state === 'done') {
        toast({ tone: 'ok', title: 'Disco listo para las copias', body: end.message });
        try { accept(await adapter().state()); } catch { /* se verá en el siguiente refresco */ }
      } else if (end) toast({ tone: 'err', title: 'No se pudo preparar el disco', body: end.message });
      onDone();
    })();
  };

  const saveDir = async () => {
    setBusy(true); setDirErr('');
    try { accept(await adapter().saveConfig({ ...cfg, backupCopyDir: dir.trim() })); toast({ tone: 'ok', title: dir.trim() ? 'Carpeta externa guardada' : 'Carpeta externa quitada', body: dir.trim() ? 'Comprueba abajo que Umbral puede escribir en ella.' : 'Las copias quedan solo en el servidor.' }); onDone(); }
    catch (e) { setDirErr(e instanceof Error ? e.message : String(e)); }
    setBusy(false);
  };
  const copyNow = async () => {
    setBusy(true);
    try { const r = await adapter().copyBackups(); toast({ tone: 'ok', title: 'Copias al día en el disco externo', body: r.copied ? r.copied + (r.copied === 1 ? ' archivo copiado.' : ' archivos copiados.') : 'Ya estaban todas.' }); onDone(); }
    catch (e) { handleErr(e, 'No se pudieron copiar'); }
    setBusy(false);
  };
  const pendingCopy = (b?.files || []).filter((f) => !f.copied).length;
  const open = (disk: ExtDisk, action: Ask['action']) => { setAsk({ disk, action }); setTyped(''); };

  return (
    <div className="pb" style={{ borderTop: '1px solid var(--line)' }}>
      <span className="strong" style={{ display: 'block' }}>Disco externo</span>
      <span className="sub" style={{ display: 'block' }}>Si el disco del servidor falla, las copias que solo están en él se pierden con los datos. Conecta un disco USB al servidor: Umbral lo detecta y lo deja listo con un botón.</span>

      {!b ? null : disks.length ? (
        <div style={{ margin: '10px calc(var(--pad) * -1) 0' }}>
          {disks.map((dk) => {
            const asking = ask?.disk.name === dk.name;
            const canUse = dk.state === 'mounted' || dk.state === 'ext4';
            return (
              <div key={dk.name}>
                <div className="lrow" style={{ flexWrap: 'wrap' }}>
                  <span className="ico-box"><Icon name="disk" /></span>
                  <div className="grow">
                    <span className="strong">{dk.model} · {gb(dk.sizeBytes)}</span>
                    <span className="sub">{dk.name} · {content(dk)}</span>
                    {dk.state === 'busy' && <span className="sub" style={{ color: 'var(--warn)' }}>{dk.problem}</span>}
                    {dk.state === 'other' && !dk.canFormat && dk.problem && <span className="sub" style={{ color: 'var(--warn)' }}>{dk.problem}</span>}
                  </div>
                  <Badge s={badge(dk)} />
                  {!asking && (canUse || dk.state === 'other') && (
                    <div className="row-actions">
                      {canUse && <button className="btn btn-sm btn-primary" disabled={!canAct} onClick={() => open(dk, 'usar')}>{dk.known && dk.state === 'ext4' ? 'Volver a montarlo' : 'Usar este disco'}</button>}
                      {dk.canFormat && <button className={'btn btn-sm ' + (canUse ? 'btn-quiet' : 'btn-danger')} disabled={!canAct} onClick={() => open(dk, 'formatear')}>{canUse ? 'Borrar y preparar…' : 'Preparar para copias…'}</button>}
                    </div>
                  )}
                </div>
                {asking && ask && (
                  <div style={{ padding: '0 var(--pad) 12px' }}>
                    {ask.action === 'formatear' ? (
                      <>
                        <Banner tone="err" icon="alert" title="Se borrará todo lo que tiene el disco">
                          {dk.model} ({gb(dk.sizeBytes)}): {content(dk)}. Quedará con una sola partición ext4, que es lo que necesitan las copias. No se puede deshacer.
                        </Banner>
                        <label className="small" style={{ display: 'block', marginTop: 12 }} htmlFor={'borrar-' + dk.name}>Escribe <b>BORRAR</b> para confirmar</label>
                        <input id={'borrar-' + dk.name} className="inp mono" style={{ width: 150, marginTop: 6 }} value={typed} onChange={(e) => setTyped(e.target.value)} autoFocus autoComplete="off" spellCheck={false} />
                      </>
                    ) : (
                      <p className="f-msg" style={{ marginTop: 4 }}>No se borra nada. Umbral lo monta en /mnt, hace que se monte solo en cada arranque y crea en él la carpeta <code>umbral</code> para las copias.</p>
                    )}
                    <Codigo2FA
                      submitLabel={ask.action === 'formatear' ? 'Borrar y preparar el disco' : 'Usar este disco'}
                      danger={ask.action === 'formatear'}
                      autoFocus={ask.action !== 'formatear'}
                      disabled={ask.action === 'formatear' && typed.trim().toUpperCase() !== 'BORRAR' ? 'Escribe BORRAR arriba para poder continuar.' : undefined}
                      onSubmit={prepare}
                      onCancel={() => { setAsk(null); setTyped(''); }}
                    />
                  </div>
                )}
              </div>
            );
          })}
        </div>
      ) : (
        <Empty icon="disk" style={{ marginTop: 10 }}>No hay ningún disco USB conectado al servidor. Al conectarlo aparecerá aquí en unos segundos.</Empty>
      )}

      {b && disks.some((x) => x.state !== 'ready' && x.state !== 'busy') && (why ? <p className="f-msg" style={{ marginTop: 8 }}>{why}</p> : !totp && <div style={{ marginTop: 8 }}><Falta2FA what="Preparar el disco" /></div>)}

      {job && (
        <ol className="job-steps">
          {job.steps.map((s) => (
            <li key={s.label} className={s.state}><Badge s={s.state === 'done' ? st('resOk', 'Hecho') : s.state === 'running' ? st('starting', 'En curso') : s.state === 'error' ? st('resErr', 'Error') : st('noCheck', 'Pendiente')} iconOnly /> {s.label}</li>
          ))}
          {job.state === 'error' && <li className="error" style={{ color: 'var(--err)' }}>{job.message}</li>}
        </ol>
      )}

      {cd && (cd.ok && !cd.sameDisk
        ? <>
            <p className="f-msg" style={{ marginTop: 12 }}>Las copias van a {cd.path}. Hay {cd.files} {cd.files === 1 ? 'copia' : 'copias'} de la base de datos{cd.lastCopy ? ', la última de ' + agoIso(cd.lastCopy).toLowerCase() : ''}{cd.freeGB >= 0 ? ' · ' + cd.freeGB + ' GB libres' : ''}. Allí se guardan las 10 más recientes.</p>
            {pendingCopy > 0 && <div className="mem-row"><button className="btn btn-sm" onClick={() => void copyNow()} disabled={busy || !!blocked}><Icon name="download" />Llevar {pendingCopy === 1 ? 'la copia pendiente' : 'las ' + pendingCopy + ' copias pendientes'} de la base de datos</button></div>}
          </>
        : <Banner tone={cd.ok ? 'warn' : 'err'} icon="alert" style={{ marginTop: 12 }}>{cd.problem}</Banner>)}

      <details className="det" style={{ marginTop: 12 }}>
        <summary>Usar otra carpeta (avanzado)</summary>
        <p className="f-msg" style={{ marginTop: 8 }}>Para un disco que montas tú mismo en /mnt o /media. Tiene que ser ext4 y la carpeta tiene que pertenecer al usuario umbral.</p>
        <div className="mem-row">
          <input className={'inp mono' + (dirErr ? ' bad' : '')} style={{ width: 'min(340px,100%)' }} value={dir} placeholder="/mnt/copias/umbral" onChange={(e) => setDir(e.target.value)} aria-label="Carpeta externa" spellCheck={false} />
          <button className="btn" onClick={() => void saveDir()} disabled={busy || !!blocked || dir.trim() === cfg.backupCopyDir}>{dir.trim() || !cfg.backupCopyDir ? 'Guardar carpeta' : 'Quitar carpeta'}</button>
        </div>
        {dirErr && <p className="f-msg bad" role="alert">{dirErr}</p>}
      </details>
    </div>
  );
}
