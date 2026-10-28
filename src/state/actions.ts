import type { Image } from '../data/types';
import { act, memLabel, type Action } from '../design/ui';
import type { CtrView, Derived } from './derive';
import { adapter, confirmModal, follow, get, nav, needsSetup, run, set, toast } from './store';

export const TOOLS = [
  { id: 'rdp', label: 'Escritorio remoto de Windows (RDP)', note: 'Incluido en Windows Pro. Se descarga un archivo .rdp que abre el cliente de tu dispositivo.' },
  { id: 'rustdesk', label: 'RustDesk', note: 'Código abierto. Se conecta por la IP del PC, dentro de tu red o de Tailscale.' },
] as const;

export const toolObj = (d: Derived) => TOOLS.find((t) => t.id === d.s.config.remoteTool) || null;

export function openUrl(label: string, url: string) {
  if (!url) { toast({ tone: 'warn', title: 'Sin dirección', body: label + ' no publica ningún puerto.' }); return; }
  window.open(url, '_blank', 'noopener,noreferrer');
}

export function sshCommand(d: Derived) {
  const u = d.s.config.sshUser;
  return 'ssh ' + (u ? u + '@' : '') + (d.s.server.ip || d.s.server.hostname);
}

export async function copySsh(d: Derived) {
  const cmd = sshCommand(d);
  try {
    await navigator.clipboard.writeText(cmd);
    toast({ tone: 'ok', title: 'Comando copiado', body: cmd + ' está en el portapapeles.' });
  } catch {
    toast({ tone: 'info', title: 'Copia el comando', body: cmd + ' (el navegador no permitió copiarlo solo: el portapapeles necesita HTTPS).' });
  }
}

export function connect(d: Derived) {
  const tool = toolObj(d), pc = d.s.pc;
  if (!tool) return;
  if (tool.id === 'rdp') {
    const rdp = 'full address:s:' + pc.ip + '\r\nprompt for credentials:i:1\r\nscreen mode id:i:2\r\n';
    const a = document.createElement('a');
    a.href = URL.createObjectURL(new Blob([rdp], { type: 'application/x-rdp' }));
    a.download = (pc.name || 'PC') + '.rdp';
    a.click();
    setTimeout(() => URL.revokeObjectURL(a.href), 2000);
    toast({ tone: 'info', title: 'Archivo de conexión descargado', body: 'Ábrelo con el cliente de Escritorio remoto. La contraseña la pide Windows, no este panel.' });
  } else {
    window.location.href = 'rustdesk://' + pc.ip;
  }
}

export async function wake() {
  if (await run('No se pudo encender el PC', () => adapter().wakePc()))
    toast({ tone: 'info', title: 'Paquete de encendido enviado', body: 'El servidor lo ha enviado a ' + get().snap?.pc.mac + '. El PC tarda un minuto en responder; su estado se actualiza solo.' });
}

export async function checkPc() {
  set({ checkingPc: true });
  const ok = await run('No se pudo comprobar el PC', () => adapter().checkPc());
  set({ checkingPc: false });
  const pc = get().snap?.pc;
  if (ok && pc) toast({ tone: pc.state === 'on' ? 'ok' : 'info', title: 'Comprobación hecha', body: pc.state === 'on' ? 'El PC responde en ' + pc.ip + '.' : 'El PC no responde en ' + pc.ip + ': está apagado o el firewall bloquea el puerto de comprobación.' });
}

export function pcActions(d: Derived) {
  const s = get();
  const pc = d.s.pc;
  const notCfg = !pc.configured;
  const wakeA = notCfg
    ? act({ label: 'Encender con Wake-on-LAN', icon: 'power', kind: 'setup', note: 'falta la IP y la MAC del PC. Añádelas en Ajustes › Dispositivos registrados.', run: () => needsSetup('Encender con Wake-on-LAN', 'el servidor necesita la IP y la MAC del PC. Puedes elegirlo entre los equipos detectados en la red.', 'settings', { setTab: 'devices' }) })
    : act({ label: 'Encender con Wake-on-LAN', icon: 'power', kind: 'primary', run: () => void wake(),
      blocked: !d.srvOn ? 'No disponible: el paquete de encendido lo envía el servidor, que no responde.' : d.pcState === 'on' ? 'El PC ya está encendido.' : d.pcState === 'waking' ? 'Encendiendo…' : '',
      note: d.srvOn && d.pcState === 'off' ? 'Lo envía ' + d.s.server.hostname + ' por la red local.' : '' });
  const check = act({ label: s.checkingPc ? 'Comprobando…' : 'Comprobar disponibilidad', icon: 'radar', spin: s.checkingPc, run: () => void checkPc(),
    blocked: notCfg ? 'Falta configurar la IP del PC.' : !d.srvOn ? 'No disponible: la comprobación se hace desde el servidor.' : s.checkingPc ? 'Comprobando…' : '' });
  const tool = toolObj(d);
  const remote = !tool
    ? act({ label: 'Abrir escritorio remoto', kind: 'setup', note: 'elige primero qué herramienta usas en Acceso remoto.', run: () => needsSetup('Abrir escritorio remoto', 'elige primero qué herramienta usas para conectarte al PC.', 'remote') })
    : act({ label: 'Conectarse con ' + tool.label.replace(' (RDP)', ''), kind: 'external', run: () => connect(d),
      blocked: d.pcState === 'on' ? '' : d.pcState === 'unknown' ? 'Estado del PC desconocido.' : d.pcState === 'notConfigured' ? 'Falta configurar el PC.' : 'El PC está apagado. Enciéndelo primero.',
      note: d.pcState === 'on' ? 'Se abrirá ' + tool.label + ' en este dispositivo.' : '' });
  return { wake: wakeA, check, remote };
}

/* ---------- Contenedores ---------- */
export function ctrAction(c: CtrView, kind: 'start' | 'stop' | 'restart' | 'remove') {
  const ncNote = c.isNextcloud ? ' Nextcloud no estará disponible para nadie mientras tanto.' : '';
  const go = async () => {
    const mid = ({ restart: 'restarting', stop: 'stopping', start: 'starting', remove: 'removing' } as const)[kind];
    set((s) => ({ busy: { ...s.busy, [c.name]: mid } }));
    const verb = { restart: 'Reiniciado', stop: 'Detenido', start: 'Iniciado', remove: 'Eliminado' }[kind];
    const ok = await run('No se pudo completar la acción en ' + c.name, () => adapter().containerAction(c.name, kind), { title: verb + ': ' + c.name, body: kind === 'remove' ? 'El contenedor se ha eliminado. Sus datos en disco y su imagen se conservan.' : 'El cambio ya se refleja en la lista.' });
    set((s) => { const b = { ...s.busy }; delete b[c.name]; return { busy: b, drawer: ok && kind === 'remove' ? null : s.drawer }; });
  };
  if (kind === 'start') { void go(); return; }
  const m = {
    restart: { title: '¿Reiniciar ' + c.name + '?', body: c.name + ' se detendrá unos segundos y volverá a arrancar. Las conexiones abiertas se cortarán.', confirmLabel: 'Reiniciar contenedor', confirmCls: 'btn btn-primary' },
    stop: { title: '¿Detener ' + c.name + '?', body: c.name + ' dejará de funcionar hasta que lo vuelvas a iniciar.' + ncNote, confirmLabel: 'Detener contenedor', confirmCls: 'btn btn-danger-solid' },
    remove: { title: '¿Eliminar el contenedor ' + c.name + '?', body: 'Se borrará el contenedor. Las carpetas de datos montadas y la imagen se conservan; para liberar el espacio de la imagen, elimínala después en Imágenes.', confirmLabel: 'Eliminar contenedor', confirmCls: 'btn btn-danger-solid' },
  }[kind];
  confirmModal({ ...m, onConfirm: go });
}

/* ---------- Imágenes ---------- */
export const imgRef = (i: Image) => (i.repo === '<none>' ? '' : i.repo + ':' + i.tag);

export function removeImages(list: Image[]) {
  const total = list.reduce((n, i) => n + i.sizeMB, 0);
  const names = list.map((i) => (i.repo === '<none>' ? 'la imagen sin etiqueta ' + i.id : imgRef(i)));
  confirmModal({
    title: list.length === 1 ? '¿Eliminar ' + (list[0].repo === '<none>' ? 'la imagen sin etiqueta' : imgRef(list[0])) + '?' : '¿Eliminar ' + list.length + ' imágenes sin usar?',
    body: (list.length === 1 ? 'Se borrará del servidor' : 'Se borrarán ' + names.join(', ')) + ' y liberará ' + memLabel(total) + '. Si la necesitas más adelante, tendrás que descargarla otra vez.',
    confirmLabel: list.length === 1 ? 'Eliminar imagen' : 'Eliminar y liberar espacio', confirmCls: 'btn btn-danger-solid',
    onConfirm: async () => {
      if (await run('No se pudo eliminar', () => adapter().removeImages(list.map((i) => ({ id: i.id, ref: imgRef(i) }))), { title: 'Espacio liberado', body: memLabel(total) + ' menos en el disco del sistema.' }))
        set({ drawer: null });
    },
  });
}

export async function startPull() {
  const p = get().pull;
  if (!p) return;
  try {
    const j = await adapter().pullImage(p.name.trim());
    const existed = !!get().snap?.docker.images.some((i) => imgRef(i) === j.target);
    set({ pull: { ...p, phase: 'pulling', ref: j.target, job: j, existed } });
    const end = await follow(j.id, (job) => set((s) => (s.pull ? { pull: { ...s.pull, job } } : {})));
    set((s) => (s.pull ? { pull: { ...s.pull, phase: end?.state === 'done' ? 'done' : 'error', job: end || s.pull.job } } : {}));
  } catch (e) {
    toast({ tone: 'err', title: 'No se pudo descargar', body: e instanceof Error ? e.message : String(e) });
  }
}

/* ---------- Actualizaciones ---------- */
export function doUpdate(name: string, image: string, revert: boolean, major?: string) {
  confirmModal(major
    ? { title: '¿Pasar Nextcloud a la versión ' + (major.split(':')[1] || major) + '?', body: 'Es un cambio de versión mayor: Nextcloud estará en modo de mantenimiento entre 10 y 30 minutos mientras actualiza su base de datos; avisa a quien lo use. Antes, el panel copiará la base de datos; si la copia falla, no se toca nada. No se puede volver atrás desde el panel. Algunas apps de Nextcloud pueden quedar desactivadas si aún no son compatibles. No reinicies el servidor mientras tanto.', confirmLabel: 'Cambiar de versión', confirmCls: 'btn btn-danger-solid', onConfirm: () => go() }
    : revert
    ? { title: '¿Volver a la versión anterior de ' + name + '?', body: 'Se recreará el contenedor con la imagen anterior, que se conservó al actualizar. Estará sin servicio unos segundos.', confirmLabel: 'Revertir', onConfirm: () => go() }
    : image.startsWith('nextcloud')
      ? { title: '¿Actualizar Nextcloud ahora?', body: 'Nextcloud estará en modo de mantenimiento entre 5 y 15 minutos: avisa a quien lo esté usando, porque una subida en curso se cortará. Antes de tocar nada, el panel copiará la base de datos; si la copia falla, no se actualiza. Tus archivos no se tocan. Si la versión nueva salta más de una versión mayor, se detiene sin cambiar nada. Si algo sale mal después, Nextcloud se queda en mantenimiento para proteger los datos. No reinicies el servidor ni actualices Debian mientras tanto.', confirmLabel: 'Actualizar Nextcloud', confirmCls: 'btn btn-danger-solid', onConfirm: () => go() }
      : { title: '¿Actualizar ' + name + '?', body: 'Se descargará la versión nueva de ' + image + ' y se recreará el contenedor con la misma configuración y los mismos datos. Estará sin servicio unos segundos. La imagen anterior se conserva para poder revertir. No reinicies el servidor mientras tanto.', confirmLabel: 'Actualizar', onConfirm: () => go() });
  async function go() {
    try {
      const j = await (major ? adapter().applyMajor(name) : revert ? adapter().revertUpdate(name) : adapter().applyUpdate(name));
      set((s) => ({ updJobs: { ...s.updJobs, [name]: j.id } }));
      void load2();
      set((s) => ({ updView: { ...s.updView, [name]: j } }));
      const end = await follow(j.id, (job) => set((s) => ({ updView: { ...s.updView, [name]: job } })));
      set((s) => { const u = { ...s.updJobs }; delete u[name]; const v = { ...s.updView }; delete v[name]; return { updJobs: u, updView: v }; });
      if (end?.state === 'done') toast({ tone: 'ok', title: revert ? 'Reversión completada' : 'Actualización completada', body: end.message });
      else if (end) toast({ tone: 'err', title: revert ? 'No se pudo revertir' : 'No se pudo actualizar', body: end.message });
    } catch (e) {
      toast({ tone: 'err', title: 'No se pudo empezar', body: e instanceof Error ? e.message : String(e) });
    }
  }
}

async function load2() { try { const s = await adapter().state(); set({ snap: s }); } catch { /* se reintenta con el sondeo */ } }

export async function backupDb(container: string) {
  set({ busyBackup: true });
  try {
    const r = await adapter().backupDatabase(container);
    toast({ tone: 'ok', title: 'Copia de seguridad hecha', body: 'Base de datos ' + r.database + ': ' + r.path + ' (' + r.sizeKB + ' KB comprimida).' });
    void load2();
  } catch (e) {
    toast({ tone: 'err', title: 'La copia de seguridad falló', body: e instanceof Error ? e.message : String(e) });
  }
  set({ busyBackup: false });
}

export function goDrawer(drawer: NonNullable<ReturnType<typeof get>['drawer']>) { set({ drawer }); }
export { nav };
export type { Action };
