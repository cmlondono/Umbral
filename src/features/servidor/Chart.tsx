import { useRef, useState } from 'react';
import type { View } from '../../data/types';
import { hhmm } from '../../design/ui';
import { sparkPath } from './charts';
import { LIVE_POINTS } from './live';

export interface Serie { label: string; values: number[]; color: string; fmt: (v: number) => string; area?: boolean; now?: string }

const SPAN: Record<View, number> = { live: LIVE_POINTS, '1h': 60, '24h': 96, '7d': 84 };
const STEP_LABEL: Record<View, string> = { live: 'Lectura en vivo', '1h': 'Media de 1 minuto', '24h': 'Media de 15 minutos', '7d': 'Media de 2 horas' };

/* Etiqueta del intervalo que representa un punto: cada punto es la media de
 * un tramo, no una lectura instantánea. */
function bucketLabel(iso: string, stepSec: number, range: View) {
  const a = new Date(iso), b = new Date(a.getTime() + stepSec * 1000);
  if (range === 'live') return a.toLocaleTimeString('es', { hour: '2-digit', minute: '2-digit', second: '2-digit' });
  const day = range === '7d' ? a.toLocaleDateString('es', { weekday: 'short', day: 'numeric' }) + ', ' : '';
  return range === '1h' ? day + hhmm(a) : day + hhmm(a) + '–' + hhmm(b);
}

/** Gráfica pequeña con lectura al pasar el ratón, el dedo o las flechas del teclado. */
export function Chart({ title, now, legend, series, times, stepSec, range, max, y, xl, note }: {
  title: string; now?: string; legend?: boolean; series: Serie[]; times: string[]; stepSec: number; range: View; max: number; y: string[]; xl: string[]; note: string;
}) {
  const box = useRef<HTMLDivElement>(null);
  const [idx, setIdx] = useState<number | null>(null);
  const n = times.length;
  const ok = n > 1;
  const xw = (600 * Math.min(n, SPAN[range])) / SPAN[range];
  const x0 = 600 - xw;
  const xAt = (i: number) => x0 + (i / Math.max(1, n - 1)) * xw;

  const pick = (clientX: number) => {
    const r = box.current?.getBoundingClientRect();
    if (!r || !ok) return;
    const vx = ((clientX - r.left) / r.width) * 600;
    if (vx < x0 - 6) { setIdx(null); return; }
    setIdx(Math.max(0, Math.min(n - 1, Math.round(((vx - x0) / xw) * (n - 1)))));
  };
  const key = (e: React.KeyboardEvent) => {
    if (!ok) return;
    if (e.key === 'ArrowLeft' || e.key === 'ArrowRight') {
      e.preventDefault();
      const d = e.key === 'ArrowLeft' ? -1 : 1;
      setIdx((i) => Math.max(0, Math.min(n - 1, (i ?? n - 1) + (i === null ? 0 : d))));
    } else if (e.key === 'Escape') setIdx(null);
  };
  const pct = idx === null ? 0 : (xAt(idx) / 600) * 100;
  const tipText = idx === null ? '' : bucketLabel(times[idx], stepSec, range) + ': ' + series.map((s) => (series.length > 1 ? s.label + ' ' : '') + s.fmt(s.values[idx])).join(', ');

  return (
    <div className="chart">
      <div className="chart-h">
        <b>{title}</b>
        {legend
          ? <span className="legend">{series.map((s) => <span key={s.label}><i style={{ background: s.color }} />{s.label}{s.now ? ' ' + s.now : ''}</span>)}</span>
          : <span className="chart-now">{now}</span>}
      </div>
      <div className="chart-body">
        <div className="chart-y">{y.map((l) => <span key={l}>{l}</span>)}</div>
        <div>
          <div className="chart-hit" ref={box} tabIndex={ok ? 0 : -1} role="img"
            aria-label={title + (ok ? '. Usa las flechas para leer cada punto.' : '. Aún sin datos.')}
            onPointerMove={(e) => pick(e.clientX)} onPointerDown={(e) => pick(e.clientX)} onPointerLeave={(e) => { if (e.pointerType === 'mouse') setIdx(null); }}
            onKeyDown={key} onBlur={() => setIdx(null)}>
            <svg className="chart-svg" viewBox="0 0 600 132" preserveAspectRatio="none" aria-hidden="true">
              <path d="M0 0.5H600M0 66H600M0 131.5H600" stroke="var(--grid)" strokeWidth="1" vectorEffect="non-scaling-stroke" />
              {ok && <g transform={'translate(' + x0 + ' 0)'}>
                {series.map((s) => (
                  <g key={s.label}>
                    {s.area && <path d={sparkPath(s.values, max, xw, 132, true)} fill="var(--accentSoft)" stroke="none" />}
                    <path d={sparkPath(s.values, max, xw, 132)} fill="none" stroke={s.color} strokeWidth="1.6" vectorEffect="non-scaling-stroke" />
                  </g>
                ))}
              </g>}
              {idx !== null && <path d={'M' + xAt(idx) + ' 0V132'} stroke="var(--ink3)" strokeWidth="1" strokeDasharray="3 3" vectorEffect="non-scaling-stroke" />}
            </svg>
            {idx !== null && series.map((s) => (
              <span key={s.label} className="chart-dot" style={{ left: pct + '%', top: (1 - Math.min(s.values[idx], max) / max) * 100 + '%', background: s.color }} />
            ))}
            {idx !== null && (
              <div className="chart-tip glass" style={{ left: pct + '%', transform: 'translateX(' + (pct > 75 ? '-100%' : pct < 25 ? '0' : '-50%') + ')' }} aria-hidden="true">
                <span className="chart-tip-t">{bucketLabel(times[idx], stepSec, range)}</span>
                {series.map((s) => (
                  <span key={s.label} className="chart-tip-v"><i style={{ background: s.color }} />{series.length > 1 ? s.label + ' ' : ''}<b>{s.fmt(s.values[idx])}</b></span>
                ))}
                <span className="chart-tip-n">{STEP_LABEL[range]}</span>
              </div>
            )}
            <span className="sr" aria-live="polite">{tipText}</span>
          </div>
          <div className="chart-x">{xl.map((l) => <span key={l}>{l}</span>)}</div>
        </div>
      </div>
      <div className="chart-note">{note}</div>
    </div>
  );
}
