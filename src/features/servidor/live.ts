import { useEffect, useState } from 'react';
import type { History } from '../../data/types';
import { loadLive } from '../../state/store';

/* Vista «En vivo»: una lectura cada 2 s mientras esté activa y la pestaña
 * visible. Guarda los últimos 5 minutos solo en el navegador. */

export const LIVE_STEP = 2;
export const LIVE_POINTS = 150;

const empty = (): History => ({ range: '1h', stepSec: LIVE_STEP, times: [], cpu: [], rx: [], tx: [], mem: [], temp: [] });

export function useLive(on: boolean): History | null {
  const [h, setH] = useState<History>(empty);

  useEffect(() => {
    if (!on) { setH(empty()); return; }
    let stop = false, busy = false;
    const tick = async () => {
      if (busy || document.hidden) return;
      busy = true;
      const p = await loadLive();
      busy = false;
      if (stop || !p) return;
      const cut = (a: number[], v: number) => [...a, v].slice(-LIVE_POINTS);
      setH((x) => ({ ...x, times: [...x.times, p.at].slice(-LIVE_POINTS), cpu: cut(x.cpu, p.cpu), rx: cut(x.rx, p.rx), tx: cut(x.tx, p.tx), mem: cut(x.mem, p.mem), temp: cut(x.temp, p.temp ?? 0) }));
    };
    void tick();
    const t = setInterval(() => void tick(), LIVE_STEP * 1000);
    const vis = () => { if (!document.hidden) void tick(); };
    document.addEventListener('visibilitychange', vis);
    return () => { stop = true; clearInterval(t); document.removeEventListener('visibilitychange', vis); };
  }, [on]);

  return on ? h : null;
}
