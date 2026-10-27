import { useEffect, useState } from 'react';
import { Icon, Seg } from './ui';

/* Paginación común a todas las tablas: 10, 20 o 100 filas por página.
 * El tamaño elegido se recuerda por tabla en este navegador. */

const SIZES = ['10', '20', '100'] as const;
type Size = (typeof SIZES)[number];

function readSize(id: string): Size {
  try {
    const v = localStorage.getItem('umbral.filas.' + id);
    if (v && (SIZES as readonly string[]).includes(v)) return v as Size;
  } catch { /* almacenamiento bloqueado */ }
  return '10';
}

export interface Paged<T> { rows: T[]; pager: React.ReactNode }

export function usePaged<T>(items: T[], id: string): Paged<T> {
  const [size, setSize] = useState<Size>(() => readSize(id));
  const [page, setPage] = useState(0);
  const per = Number(size);
  const total = items.length;
  const pages = Math.max(1, Math.ceil(total / per));
  // Si la lista se acorta (por un filtro o una búsqueda), no se queda en una página vacía.
  useEffect(() => { if (page > pages - 1) setPage(pages - 1); }, [page, pages]);
  const p = Math.min(page, pages - 1);
  const from = p * per;
  const rows = items.slice(from, from + per);
  const choose = (v: Size) => {
    setSize(v);
    setPage(0);
    try { localStorage.setItem('umbral.filas.' + id, v); } catch { /* almacenamiento bloqueado */ }
  };
  const pager = (
    <div className="pager" role="navigation" aria-label="Paginación">
      <span className="pager-info">{total ? (from + 1) + '–' + (from + rows.length) + ' de ' + total : 'Sin registros'}</span>
      <span className="pager-size"><span className="small muted">Filas</span><Seg label="Filas por página" value={size} options={SIZES.map((s) => [s, s] as [Size, string])} onChange={choose} /></span>
      <span className="pager-nav">
        <button className="btn btn-sm btn-quiet btn-icon" onClick={() => setPage(p - 1)} disabled={p === 0} aria-label="Página anterior"><Icon name="chev" style={{ transform: 'rotate(180deg)' }} /></button>
        <span className="small" aria-live="polite">Página {p + 1} de {pages}</span>
        <button className="btn btn-sm btn-quiet btn-icon" onClick={() => setPage(p + 1)} disabled={p >= pages - 1} aria-label="Página siguiente"><Icon name="chev" /></button>
      </span>
    </div>
  );
  return { rows, pager };
}
