import { useState } from 'react';
import type { NetDevice } from '../../data/types';
import { adapter, confirmModal, nav, run, set } from '../../state/store';

/* Ficha de un equipo del mapa: cambiarle el nombre o quitarlo. */
export function DeviceExtra({ v, blocked }: { v: NetDevice; blocked: string }) {
  const [name, setName] = useState(v.custom || v.name);
  const save = () => void run('No se pudo guardar', () => adapter().updateDevice(v.mac, name.trim(), true), { title: 'Nombre guardado', body: name.trim() || 'Vuelve al nombre detectado.' });
  const remove = () => confirmModal({
    title: '¿Quitar ' + (v.name || v.mac) + ' del mapa?', body: 'Deja de aparecer en Inicio y en Dispositivos. Seguirá en Red, como cualquier equipo detectado, y podrás volver a añadirlo.',
    confirmLabel: 'Quitar del mapa',
    onConfirm: async () => { if (await run('No se pudo quitar', () => adapter().updateDevice(v.mac, v.custom, false), { title: 'Quitado del mapa', body: (v.name || v.mac) + '.' })) set({ drawer: null }); },
  });
  return (
    <div className="ag">
      <h3>Nombre</h3>
      <p>El que verás en todo el panel. Vacío, vuelve al que anuncia el equipo.</p>
      <div className="mem-row" style={{ marginTop: 0 }}>
        <input className="inp" style={{ width: 'min(260px,100%)' }} value={name} maxLength={40} onChange={(e) => setName(e.target.value)} aria-label="Nombre del equipo" onKeyDown={(e) => { if (e.key === 'Enter') save(); }} />
        <button className="btn btn-primary" onClick={save} disabled={!!blocked || name.trim() === (v.custom || v.name)}>Guardar nombre</button>
      </div>
      <div className="ag-list" style={{ marginTop: 14 }}>
        <div className="ag-item"><button className="btn" onClick={() => { set({ drawer: null }); nav('network'); }}>Ver en Red</button></div>
        <div className="ag-item"><button className="btn btn-quiet btn-danger" onClick={remove} disabled={!!blocked}>Quitar del mapa</button></div>
      </div>
    </div>
  );
}
