import { useState } from 'react';
import type { FileSnapshot, Job } from '../../data/types';
import type { Derived } from '../../state/derive';
import { accept, adapter, follow, handleErr, toast } from '../../state/store';
import { Badge, Icon, Seg, Switch, agoIso, dur, memLabel, st } from '../../design/ui';
import { usePaged } from '../../design/Pager';

/* Copia completa de Nextcloud (base de datos y archivos) al disco externo,
 * en instantáneas que solo ocupan lo que cambia. */
export function CopiaArchivos({ d, snaps, onDone }: { d: Derived; snaps: FileSnapshot[]; onDone: () => void }) {
  const cfg = d.s.config;
  const [job, setJob] = useState<Job | null>(null);
  const pg = usePaged(snaps, 'instantaneas');
  const blocked = d.serverBlocked;
  const running = !!job && job.state === 'running';
  const last = snaps.find((x) => x.ok);
  const dst = cfg.backupCopyDir + '/nextcloud-archivos';
  const dbc = d.s.docker.containers.find((c) => /^(mariadb|mysql|postgres)/.test(c.image.split('/').pop() || ''));
  const restoreDb = 'gunzip -c COPIA.sql.gz | docker exec -i ' + (dbc?.name || 'CONTENEDOR-DB') +
    (dbc?.image.includes('postgres') ? ' psql -U postgres' : ' sh -c \'exec mariadb -u"${MARIADB_USER:-$MYSQL_USER}" -p"${MARIADB_PASSWORD:-$MYSQL_PASSWORD}" "${MARIADB_DATABASE:-$MYSQL_DATABASE}"\'');

  const start = async () => {
    try {
      const j = await adapter().backupNextcloud();
      setJob(j);
      const end = await follow(j.id, setJob);
      if (end?.state === 'done') toast({ tone: 'ok', title: 'Copia completa hecha', body: end.message });
      else if (end) toast({ tone: 'err', title: 'La copia no terminó', body: end.message });
      onDone();
    } catch (e) { handleErr(e, 'No se pudo empezar la copia'); }
  };
  const saveOpt = (p: Partial<typeof cfg>) => void adapter().saveConfig({ ...cfg, ...p }).then(accept).catch((e) => handleErr(e, 'No se pudo guardar'));

  return (
    <div className="pb" style={{ borderTop: '1px solid var(--line)' }}>
      <div style={{ display: 'flex', gap: 12, alignItems: 'flex-start', flexWrap: 'wrap' }}>
        <div className="grow" style={{ minWidth: 220 }}>
          <span className="strong" style={{ display: 'block' }}>Copia completa de Nextcloud</span>
          <span className="sub" style={{ display: 'block' }}>La base de datos y todos los archivos de los usuarios, en {dst}. La primera vez lo copia todo; después solo lo nuevo o cambiado, y cada copia se puede restaurar entera.</span>
        </div>
        <button className="btn btn-primary" onClick={() => void start()} disabled={!!blocked || running}><Icon name={running ? 'loader' : 'download'} className={running ? 'i spin' : 'i'} />{running ? 'Copiando…' : 'Hacer copia completa ahora'}</button>
      </div>

      {job && (
        <ol className="job-steps">
          {job.steps.map((s) => (
            <li key={s.label} className={s.state}><Badge s={s.state === 'done' ? st('resOk', 'Hecho') : s.state === 'running' ? st('starting', 'En curso') : s.state === 'error' ? st('resErr', 'Error') : st('noCheck', 'Pendiente')} iconOnly /> {s.label}</li>
          ))}
          {job.state === 'error' && <li className="error" style={{ color: 'var(--err)' }}>{job.message}</li>}
        </ol>
      )}

      <div className="mem-row" style={{ marginTop: 14 }}>
        <span className="small muted">Guardar las últimas</span>
        <Seg label="Copias que se guardan" value={String(cfg.ncBackupKeep || 7)} onChange={(v) => saveOpt({ ncBackupKeep: Number(v) })} options={[['3', '3'], ['7', '7'], ['14', '14'], ['30', '30']]} />
        <span className="logs-sw" style={{ marginLeft: 8 }}><Switch on={cfg.ncBackupMaint} onChange={(v) => saveOpt({ ncBackupMaint: v })} label="Modo de mantenimiento durante la copia" disabled={!!blocked} /><span className="small">Mantenimiento durante la copia</span></span>
      </div>
      <p className="f-msg">{cfg.ncBackupMaint ? 'Nextcloud no se podrá usar mientras copia: la copia es exacta aunque alguien esté subiendo archivos.' : 'Nextcloud sigue funcionando mientras copia. Un archivo que alguien suba justo en ese momento puede quedar para la copia siguiente.'} Para hacerla cada noche, usa la plantilla «Copia nocturna de Nextcloud» en Automatizaciones.</p>

      {snaps.length > 0 && (
        <>
          <div className="tbox" style={{ marginTop: 12 }}>
            <table className="tbl">
              <thead><tr><th>Copia</th><th>Resultado</th><th className="num hide-sm">Copiado</th><th className="num hide-sm">Duración</th></tr></thead>
              <tbody>
                {pg.rows.map((x) => (
                  <tr key={x.name + x.at}>
                    <td><span className="strong">{agoIso(x.at)}</span><span className="sub mono">{x.name}{x.ok && !x.exists ? ' · ya rotada' : ''}</span></td>
                    <td><Badge s={x.ok ? st('resOk', 'Completa') : st('resErr', 'Falló')} />{!x.ok && <span className="sub" style={{ color: 'var(--err)' }}>{x.error}</span>}</td>
                    <td className="num hide-sm">{x.ok ? memLabel(x.transferredMB) : '—'}<span className="sub">{x.ok ? x.files + ' archivos nuevos' : ''}</span></td>
                    <td className="num hide-sm">{dur(Math.max(60, x.seconds))}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {pg.pager}
        </>
      )}
      {last && <p className="f-msg">Última copia buena {agoIso(last.at).toLowerCase()}: {memLabel(last.totalMB)} de archivos en total.</p>}

      <details className="det" style={{ marginTop: 12 }}>
        <summary>Cómo restaurar desde el disco externo</summary>
        <ol className="small" style={{ margin: '8px 0 0', paddingLeft: 18, color: 'var(--ink2)', lineHeight: 1.6 }}>
          <li>Elige la copia (carpeta con su fecha) en <code>{dst}</code>.</li>
          <li>Pon Nextcloud en mantenimiento: <code>docker exec -u www-data {d.nc?.name || 'nextcloud'} php occ maintenance:mode --on</code></li>
          <li>Devuelve los archivos a su sitio con rsync desde la copia elegida (carpetas <code>data</code> y <code>config</code>) hacia los volúmenes de Nextcloud, conservando el dueño <code>www-data</code>.</li>
          <li>Restaura la base de datos de esa misma noche: está en <code>{cfg.backupCopyDir}</code> con el nombre de la base de datos y la fecha. Por ejemplo: <code>{restoreDb}</code></li>
          <li>Comprueba y sal del mantenimiento: <code>occ maintenance:data-fingerprint</code>, <code>occ files:scan --all</code> y <code>occ maintenance:mode --off</code></li>
        </ol>
        <p className="small muted" style={{ margin: '8px 0 0' }}>Por seguridad, el panel no restaura solo: un clic equivocado sobrescribiría los datos de todos los usuarios.</p>
      </details>
    </div>
  );
}
