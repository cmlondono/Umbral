import { useEffect, useState } from 'react';
import type { Backups } from '../../data/types';
import type { Derived } from '../../state/derive';
import { accept, adapter, handleErr, toast, useStore } from '../../state/store';
import { Badge, Banner, Empty, Icon, Panel, agoIso, memLabel, st } from '../../design/ui';
import { usePaged } from '../../design/Pager';
import { CopiaArchivos } from './CopiaArchivos';

/* Copias de seguridad de la base de datos de Nextcloud: descargarlas y
 * copiarlas a un disco externo, para que no vivan solo en el servidor. */
export function Copias({ d }: { d: Derived }) {
  const lastOk = useStore().lastOk;
  const cfg = d.s.config;
  const [b, setB] = useState<Backups | null>(null);
  const [dir, setDir] = useState(cfg.backupCopyDir);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const pg = usePaged(b?.files || [], 'copias');
  const blocked = d.serverBlocked;

  const [tick, setTick] = useState(0);
  useEffect(() => {
    let off = false;
    adapter().backups().then((r) => { if (!off) setB(r); }).catch((e) => { if (!off) handleErr(e, 'No se pudieron leer las copias'); });
    return () => { off = true; };
  }, [lastOk, cfg.backupCopyDir, tick]);
  useEffect(() => setDir(cfg.backupCopyDir), [cfg.backupCopyDir]);

  const saveDir = async () => {
    setBusy(true); setErr('');
    try { accept(await adapter().saveConfig({ ...cfg, backupCopyDir: dir.trim() })); toast({ tone: 'ok', title: dir.trim() ? 'Carpeta externa guardada' : 'Carpeta externa quitada', body: dir.trim() ? 'Comprueba abajo que Umbral puede escribir en ella.' : 'Las copias quedan solo en el servidor.' }); }
    catch (e) { setErr(e instanceof Error ? e.message : String(e)); }
    setBusy(false);
  };
  const copyNow = async () => {
    setBusy(true);
    try { const r = await adapter().copyBackups(); setB(r); toast({ tone: 'ok', title: 'Copias al día en el disco externo', body: r.copied ? r.copied + (r.copied === 1 ? ' archivo copiado.' : ' archivos copiados.') : 'Ya estaban todas.' }); }
    catch (e) { handleErr(e, 'No se pudieron copiar'); }
    setBusy(false);
  };

  const cd = b?.copyDir;
  const pendingCopy = (b?.files || []).filter((f) => !f.copied).length;

  return (
    <Panel title="Copias de seguridad" id="bk-h" hint={'Nextcloud: base de datos en ' + cfg.dataRoot + '/copias (las 5 últimas) y, con disco externo, también los archivos'}>
      {!b ? <Empty icon="clock">Leyendo…</Empty> : b.files.length ? (
        <div className="tbox">
          <table className="tbl">
            <thead><tr><th>Copia</th><th className="num">Tamaño</th>{cd && <th>Disco externo</th>}<th><span className="sr">Acciones</span></th></tr></thead>
            <tbody>
              {pg.rows.map((f) => (
                <tr key={f.name}>
                  <td><span className="strong">{agoIso(f.at)}</span><span className="sub mono">{f.name}</span></td>
                  <td className="num">{memLabel(f.sizeKB / 1024)}</td>
                  {cd && <td><Badge s={f.copied ? st('ok', 'Copiada') : st('noCheck', 'Sin copiar')} /></td>}
                  <td><div className="row-actions"><a className="btn btn-sm" href={'/api/backups/' + encodeURIComponent(f.name)} download={f.name}><Icon name="download" />Descargar</a></div></td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : <Empty icon="download">Aún no hay copias. Se hacen antes de cada actualización de Nextcloud, desde su ficha en Servicios o con la automatización «Copia nocturna de Nextcloud».</Empty>}
      {(b?.files.length || 0) > 0 && pg.pager}

      <div className="pb" style={{ borderTop: '1px solid var(--line)' }}>
        <span className="strong" style={{ display: 'block' }}>Disco externo</span>
        <span className="sub" style={{ display: 'block', marginBottom: 10 }}>Si el disco del servidor falla, las copias que solo están en él se pierden con los datos. Conecta un disco USB al servidor, móntalo en /mnt o /media y elige una carpeta suya.</span>
        <div className="mem-row" style={{ marginTop: 0 }}>
          <input className={'inp mono' + (err ? ' bad' : '')} style={{ width: 'min(340px,100%)' }} value={dir} placeholder="/mnt/copias/umbral" onChange={(e) => setDir(e.target.value)} aria-label="Carpeta externa" spellCheck={false} />
          <button className="btn" onClick={() => void saveDir()} disabled={busy || !!blocked || dir.trim() === cfg.backupCopyDir}>{dir.trim() || !cfg.backupCopyDir ? 'Guardar carpeta' : 'Quitar carpeta'}</button>
          {cd?.ok && <button className="btn btn-primary" onClick={() => void copyNow()} disabled={busy || !!blocked || !b?.files.length}><Icon name="download" />{pendingCopy ? 'Copiar ' + pendingCopy + (pendingCopy === 1 ? ' copia' : ' copias') + ' de la base de datos' : 'Comprobar copia'}</button>}
        </div>
        {err && <p className="f-msg bad" role="alert">{err}</p>}
        {cd && (cd.ok && !cd.sameDisk
          ? <p className="f-msg">Umbral puede escribir en {cd.path}. Hay {cd.files} {cd.files === 1 ? 'copia' : 'copias'}{cd.lastCopy ? ', la última de ' + agoIso(cd.lastCopy).toLowerCase() : ''}{cd.freeGB >= 0 ? ' · ' + cd.freeGB + ' GB libres' : ''}. Allí se guardan las 10 más recientes.</p>
          : <Banner tone={cd.ok ? 'warn' : 'err'} icon="alert" style={{ marginTop: 12 }}>{cd.problem}</Banner>)}
        {!cd?.ok && (
          <details className="det" style={{ marginTop: 12 }} open={!cfg.backupCopyDir}>
            <summary>Cómo preparar el disco externo (una sola vez)</summary>
            <ol className="small" style={{ margin: '8px 0 0', paddingLeft: 18, color: 'var(--ink2)', lineHeight: 1.7 }}>
              <li>Conecta el disco al servidor y búscalo por su tamaño: <code>lsblk -o NAME,SIZE,MODEL,FSTYPE,MOUNTPOINT</code>. Anota su nombre (por ejemplo, <code>sdb</code>) y asegúrate de que no es el disco del sistema ni el de los datos.</li>
              <li><b>Formatearlo borra todo lo que tenga.</b> Cambia <code>sdX</code> por su nombre: <code>sudo apt install parted</code>, <code>sudo wipefs -a /dev/sdX</code>, <code>sudo parted -s /dev/sdX mklabel gpt mkpart copias ext4 0% 100%</code> y <code>sudo mkfs.ext4 -L copias /dev/sdX1</code>. Tiene que ser ext4: con exFAT o NTFS cada copia ocuparía todo de nuevo.</li>
              <li>Que se monte solo al arrancar: <code>sudo mkdir -p /mnt/copias</code>, <code>echo 'LABEL=copias /mnt/copias ext4 defaults,nofail,x-systemd.device-timeout=10s 0 2' | sudo tee -a /etc/fstab</code>, <code>sudo systemctl daemon-reload</code> y <code>sudo mount /mnt/copias</code>.</li>
              <li>La carpeta de Umbral: <code>sudo mkdir -p /mnt/copias/umbral</code> y <code>sudo chown umbral: /mnt/copias/umbral</code>.</li>
              <li>Escribe <code>/mnt/copias/umbral</code> arriba y pulsa «Guardar carpeta». Después podrás hacer la primera copia completa y programarla cada noche con la plantilla «Copia nocturna de Nextcloud».</li>
            </ol>
            <p className="small muted" style={{ margin: '8px 0 0' }}>La guía completa, con la restauración paso a paso, está en docs/copias-de-seguridad.md del repositorio de Umbral.</p>
          </details>
        )}
      </div>
      {cd?.ok && !cd.sameDisk && <CopiaArchivos d={d} snaps={b?.ncFiles || []} onDone={() => setTick((n) => n + 1)} />}
    </Panel>
  );
}
