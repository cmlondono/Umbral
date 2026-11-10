import { useCallback, useEffect, useState } from 'react';
import type { Backups } from '../../data/types';
import type { Derived } from '../../state/derive';
import { adapter, handleErr, useStore } from '../../state/store';
import { Badge, Empty, Icon, Panel, agoIso, memLabel, st } from '../../design/ui';
import { usePaged } from '../../design/Pager';
import { CopiaArchivos } from './CopiaArchivos';
import { DiscoExterno } from './DiscoExterno';

/* Copias de seguridad de la base de datos de Nextcloud: descargarlas y
 * copiarlas a un disco externo, para que no vivan solo en el servidor. */
export function Copias({ d }: { d: Derived }) {
  const lastOk = useStore().lastOk;
  const cfg = d.s.config;
  const [b, setB] = useState<Backups | null>(null);
  const pg = usePaged(b?.files || [], 'copias');

  const [tick, setTick] = useState(0);
  const reload = useCallback(() => setTick((n) => n + 1), []);
  useEffect(() => {
    let off = false;
    adapter().backups().then((r) => { if (!off) setB(r); }).catch((e) => { if (!off) handleErr(e, 'No se pudieron leer las copias'); });
    return () => { off = true; };
  }, [lastOk, cfg.backupCopyDir, tick]);

  const cd = b?.copyDir;

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

      <DiscoExterno d={d} b={b} onDone={reload} />
      {cd?.ok && !cd.sameDisk && <CopiaArchivos d={d} snaps={b?.ncFiles || []} onDone={reload} />}
    </Panel>
  );
}
