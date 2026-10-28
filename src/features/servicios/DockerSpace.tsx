import { useState } from 'react';
import type { DockerDisk } from '../../data/types';
import { adapter, confirmModal, handleErr, toast } from '../../state/store';
import { Badge, Empty, Icon, Panel, agoIso, memLabel, st } from '../../design/ui';
import { usePaged } from '../../design/Pager';

/* Espacio que ocupa Docker más allá de las imágenes: la caché de
 * compilación y los volúmenes que ningún contenedor usa. Calcularlo cuesta
 * unos segundos, así que solo se hace al pedirlo. */
export function DockerSpace({ blocked }: { blocked: string }) {
  const [disk, setDisk] = useState<DockerDisk | null>(null);
  const [busy, setBusy] = useState(false);
  const pg = usePaged(disk?.unused || [], 'volumenes');

  const analyze = async () => {
    setBusy(true);
    try { setDisk(await adapter().dockerDisk()); } catch (e) { handleErr(e, 'No se pudo calcular el espacio'); }
    setBusy(false);
  };
  const prune = () => confirmModal({
    title: '¿Liberar ' + memLabel(disk!.buildCacheMB) + ' de caché de compilación?',
    body: 'Son restos de imágenes construidas en este servidor. No afecta a ningún contenedor ni a sus datos; la próxima compilación tardará algo más.',
    confirmLabel: 'Liberar caché',
    onConfirm: async () => {
      try { const r = await adapter().pruneBuildCache(); toast({ tone: 'ok', title: 'Caché liberada', body: memLabel(r.freedMB) + ' recuperados.' }); }
      catch (e) { handleErr(e, 'No se pudo liberar la caché'); }
      await analyze();
    },
  });
  const removeVol = (name: string, sizeMB: number, anonymous: boolean) => confirmModal({
    title: '¿Eliminar el volumen ' + (anonymous ? 'sin nombre' : name) + '?',
    body: 'Se borran para siempre los ' + (sizeMB >= 0 ? memLabel(sizeMB) : 'datos') + ' que contiene. Ningún contenedor lo usa ahora, pero puede tener datos de un servicio que eliminaste y quieras recuperar. Si no sabes qué es, no lo borres.',
    confirmLabel: 'Eliminar volumen', confirmCls: 'btn btn-danger-solid',
    onConfirm: async () => {
      try { await adapter().removeVolume(name); toast({ tone: 'ok', title: 'Volumen eliminado', body: anonymous ? name.slice(0, 12) + '…' : name }); }
      catch (e) { handleErr(e, 'No se pudo eliminar'); }
      await analyze();
    },
  });

  return (
    <Panel title="Espacio de Docker" id="space-h" hint={disk ? 'Imágenes ' + memLabel(disk.imagesMB) + ' · volúmenes ' + memLabel(disk.volumesMB) : 'Caché de compilación y volúmenes sin usar'}
      right={<button className="btn btn-sm" onClick={() => void analyze()} disabled={busy || !!blocked} title={blocked || undefined}><Icon name="refresh" />{busy ? 'Calculando…' : disk ? 'Volver a calcular' : 'Calcular espacio'}</button>}>
      {!disk ? (
        <Empty icon="disk">Calcula cuánto ocupa Docker y qué se puede liberar. Tarda unos segundos.</Empty>
      ) : (
        <>
          <div className="lrow">
            <span className="ico-box"><Icon name="boxes" /></span>
            <div className="grow"><span className="strong">Caché de compilación</span><span className="sub">{disk.buildCacheMB ? memLabel(disk.buildCacheMB) + ' que no usa ninguna compilación en curso' : 'Vacía'}</span></div>
            <button className="btn btn-sm" onClick={prune} disabled={!!blocked || !disk.buildCacheMB}>{disk.buildCacheMB ? 'Liberar ' + memLabel(disk.buildCacheMB) : 'Nada que liberar'}</button>
          </div>
          {disk.unused.length ? (
            <div className="tbox" style={{ borderTop: '1px solid var(--line)' }}>
              <table className="tbl">
                <thead><tr><th>Volumen sin usar</th><th className="num">Tamaño</th><th className="hide-sm">Creado</th><th><span className="sr">Acciones</span></th></tr></thead>
                <tbody>
                  {pg.rows.map((v) => (
                    <tr key={v.name}>
                      <td><span className="strong mono">{v.anonymous ? v.name.slice(0, 12) + '…' : v.name}</span><span className="sub">{v.anonymous ? 'Sin nombre: lo creó una imagen' : v.stack ? 'De la pila ' + v.stack : 'Con nombre'}</span></td>
                      <td className="num">{v.sizeMB >= 0 ? memLabel(v.sizeMB) : '—'}</td>
                      <td className="hide-sm nowrap">{agoIso(v.createdAt)}</td>
                      <td><div className="row-actions"><button className="btn btn-sm btn-quiet btn-danger" onClick={() => removeVol(v.name, v.sizeMB, v.anonymous)} disabled={!!blocked}>Eliminar</button></div></td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <div className="lrow" style={{ borderTop: '1px solid var(--line)' }}><Badge s={st('ok', 'Ningún volumen sin usar')} /></div>
          )}
          {disk.unused.length > 0 && pg.pager}
        </>
      )}
    </Panel>
  );
}
