import type { Container, Snapshot } from '../data/types';
import { hhmm, memLabel, st, type StateInfo, type StateKey } from '../design/ui';
import type { Page, State } from './store';

export interface Alert { sev: 'err' | 'warn'; title: string; detail: string; action: string; page: Page; drawer?: State['drawer']; extra?: Partial<State> }

export interface CtrView extends Container {
  shown: StateKey;
  app: StateKey;
  appLabel?: string;
  isNextcloud: boolean;
  issue: boolean;
}

export interface Derived {
  s: Snapshot;
  srvOn: boolean;
  lastKnown: string;
  dockerOk: boolean;
  pcState: 'on' | 'off' | 'waking' | 'unknown' | 'notConfigured';
  ctrs: CtrView[];
  running: number;
  nc: CtrView | null;
  ncState: StateInfo;
  ncAppDetail: string;
  ncUsable: boolean;
  ncUrl: string;
  hasNC: boolean;
  alerts: Alert[];
  level: 'ok' | 'warn' | 'err';
  title: string;
  detail: string;
  netdata: CtrView | null;
  tsOk: boolean;
  diskMax: { pct: number; label: string; freeGB: number; mount: string } | null;
  memPct: number;
  updatesAvail: number;
  serverBlocked: string;
}

function appState(c: Container, s: Snapshot, isNc: boolean): [StateKey, string?] {
  if (c.state !== 'running') return ['na'];
  if (isNc && s.nextcloud.configured) {
    const n = s.nextcloud;
    if (!n.reachable) return ['appFail'];
    if (n.maintenance) return ['appFail', 'Mantenimiento'];
    if (n.httpStatus !== 200) return ['appFail'];
    return ['appOk'];
  }
  return ({ healthy: ['healthy'], unhealthy: ['unhealthy'], starting: ['healthStarting'], none: ['noCheck'] } as const)[c.health] as [StateKey];
}

export function derive(st0: State): Derived | null {
  const s = st0.snap;
  if (!s) return null;
  const srvOn = st0.online;
  const lastKnown = hhmm(st0.lastOk || s.at);
  const dockerOk = srvOn && s.docker.available;
  const pcState = !s.pc.configured ? 'notConfigured' : srvOn ? s.pc.state : 'unknown';
  const ncName = s.nextcloud.container;

  const ctrs: CtrView[] = s.docker.containers.map((c) => {
    const isNc = c.name === ncName;
    const busy = st0.busy[c.name];
    let shown: StateKey = !srvOn ? 'unknown' : busy || (c.state === 'running' ? 'running' : c.state === 'stopped' ? 'stopped' : c.state);
    if (srvOn && !busy && c.state === 'restarting') shown = 'restarting';
    const [app, appLabel] = !srvOn ? (['unknown'] as [StateKey]) : appState(c, s, isNc);
    const issue = !srvOn || app === 'appFail' || app === 'unhealthy' || c.state === 'dead' || c.state === 'restarting';
    return { ...c, shown, app, appLabel, isNextcloud: isNc, issue };
  });
  const running = ctrs.filter((c) => c.state === 'running').length;
  const nc = ctrs.find((c) => c.isNextcloud) || null;
  const ncUrl = s.nextcloud.url || nc?.url || '';
  const ncUsable = srvOn && !!nc && nc.state === 'running' && nc.app === 'appOk';
  const ncState = !srvOn ? st('unknown') : !nc ? st('notConfigured', 'No encontrado') : nc.state !== 'running' ? st(nc.shown) : st(nc.app, nc.appLabel);
  const n = s.nextcloud;
  const ncAppDetail = !srvOn ? 'Sin datos: el servidor no responde'
    : !nc ? 'No hay ningún contenedor llamado ' + ncName
    : nc.state !== 'running' ? 'El contenedor no está en ejecución'
    : !n.configured ? 'Sin dirección para comprobarlo'
    : !n.reachable ? (n.error || 'No responde')
    : n.maintenance ? 'HTTP ' + n.httpStatus + ': modo de mantenimiento activo'
    : n.httpStatus !== 200 ? 'HTTP ' + n.httpStatus + ' en /status.php'
    : 'HTTP 200 en /status.php';
  const netdata = ctrs.find((c) => c.image.startsWith('netdata/')) || null;
  const tsOk = srvOn && s.tailscale.running;

  const m = s.metrics;
  const disks = m?.disks || [];
  const worst = disks.map((d) => ({ pct: d.totalGB ? Math.round((d.usedGB / d.totalGB) * 100) : 0, label: d.label, freeGB: Math.round(d.totalGB - d.usedGB), mount: d.mount }))
    .sort((a, b) => b.pct - a.pct)[0] || null;
  const memPct = m ? Math.round((m.mem.usedMB / m.mem.totalMB) * 100) : 0;
  const updatesAvail = s.updates.items.filter((u) => u.state === 'available').length;

  const alerts: Alert[] = [];
  if (!srvOn) alerts.push({ sev: 'err', title: 'El servidor no responde desde las ' + lastKnown, detail: 'Lo que ves es el último estado conocido. Comprueba que el servidor tenga corriente y cable de red, y que el servicio umbral esté en marcha.', action: 'Ver servidor', page: 'server' });
  if (srvOn && !s.docker.available) alerts.push({ sev: 'err', title: 'Docker no responde', detail: s.docker.error || 'No se puede leer el estado de los contenedores.', action: 'Ver servicios', page: 'services' });
  if (srvOn && nc && nc.state === 'running' && nc.app === 'appFail') alerts.push({ sev: 'err', title: 'Nextcloud no responde', detail: 'El contenedor está en ejecución, pero la aplicación no responde bien (' + ncAppDetail + ').', action: 'Ver Nextcloud', page: 'services', drawer: { type: 'service', id: nc.name } });
  if (srvOn && nc && nc.state !== 'running') alerts.push({ sev: 'err', title: 'Nextcloud no está disponible', detail: 'El contenedor ' + nc.name + ' está ' + st(nc.shown).label.toLowerCase() + '.', action: 'Ver Nextcloud', page: 'services', drawer: { type: 'service', id: nc.name } });
  ctrs.filter((c) => srvOn && !c.isNextcloud && (c.app === 'unhealthy' || c.state === 'dead' || c.state === 'restarting')).forEach((c) =>
    alerts.push({ sev: 'err', title: c.name + (c.state === 'restarting' ? ' se reinicia una y otra vez' : ' tiene un problema'), detail: c.status, action: 'Ver contenedor', page: 'services', drawer: { type: 'service', id: c.name } }));
  if (srvOn && s.metricsError) alerts.push({ sev: 'warn', title: 'No se pueden leer las métricas', detail: s.metricsError, action: 'Ver servidor', page: 'server' });
  if (srvOn && worst && worst.pct >= 80) alerts.push({ sev: worst.pct >= 90 ? 'err' : 'warn', title: 'El volumen ' + worst.label + ' está al ' + worst.pct + ' %', detail: 'Quedan ' + worst.freeGB + ' GB libres en ' + worst.mount + '. Conviene liberar espacio antes de llegar al 90 %.', action: 'Ver almacenamiento', page: 'server' });
  if (srvOn && m?.tempC != null && m.tempC >= 85) alerts.push({ sev: m.tempC >= 95 ? 'err' : 'warn', title: 'El procesador está a ' + Math.round(m.tempC) + ' °C', detail: 'Es más de lo normal. Revisa que el servidor tenga ventilación y que no esté cubierto ni junto a una fuente de calor.', action: 'Ver servidor', page: 'server' });
  if (srvOn && memPct >= 85) alerts.push({ sev: 'warn', title: 'La memoria está al ' + memPct + ' %', detail: 'Revisa qué contenedores consumen más antes de desplegar otro servicio.', action: 'Ver servicios', page: 'services' });
  if (srvOn && updatesAvail) alerts.push({ sev: 'warn', title: updatesAvail === 1 ? 'Hay una actualización de imagen' : 'Hay ' + updatesAvail + ' actualizaciones de imágenes', detail: 'Se ha publicado una versión nueva con la misma etiqueta. Actualizar recrea el contenedor y permite revertir.', action: 'Ver actualizaciones', page: 'services', extra: { svcTab: 'updates' } });
  if (srvOn && netdata && netdata.state === 'running') alerts.push({ sev: 'warn', title: 'Netdata usa ' + (netdata.memMB != null ? memLabel(netdata.memMB) : 'memoria') + ' sin que lo consultes', detail: 'Este panel ya muestra CPU, memoria, discos y red. Detenerlo y eliminarlo libera esa memoria para tus despliegues.', action: 'Ver Netdata', page: 'services', drawer: { type: 'service', id: netdata.name } });
  const major = s.updates.items.find((u) => u.nextMajor && u.state !== 'updating');
  if (srvOn && major) alerts.push({ sev: 'warn', title: 'Hay una versión mayor nueva de Nextcloud', detail: major.nextMajor + ' ya está publicada. Pasar a ella con el panel es seguro: copia la base de datos antes y se detiene si algo falla.', action: 'Ver actualizaciones', page: 'services', extra: { svcTab: 'updates' } });
  const sys = s.system;
  if (srvOn && sys?.security) alerts.push({ sev: 'warn', title: sys.security === 1 ? 'Hay una actualización de seguridad de Debian' : 'Hay ' + sys.security + ' actualizaciones de seguridad de Debian', detail: 'Corrigen fallos de seguridad del sistema operativo del servidor. Instálalas cuanto antes.', action: 'Ver sistema', page: 'server' });
  if (srvOn && sys?.reboot) alerts.push({ sev: 'warn', title: 'El servidor necesita reiniciarse', detail: 'Una actualización instalada (' + (sys.rebootPkgs?.slice(0, 3).join(', ') || 'del sistema') + ') no se aplica del todo hasta reiniciar. Hazlo cuando nadie esté usando Nextcloud.', action: 'Ver sistema', page: 'server' });
  (sys?.smart?.disks || []).filter((k) => k.health === 'fail' || k.health === 'warn').forEach((k) =>
    alerts.push({ sev: k.health === 'fail' ? 'err' : 'warn', title: 'El disco ' + k.name + (k.health === 'fail' ? ' está fallando' : ' muestra señales de desgaste'), detail: (k.model ? k.model + '. ' : '') + 'Haz una copia de tus datos a otro disco cuanto antes.', action: 'Ver discos', page: 'server' }));
  if (srvOn && !s.tailscale.running) alerts.push({ sev: 'warn', title: s.tailscale.installed ? 'Tailscale no está conectado' : 'Tailscale no está instalado en el servidor', detail: 'Sin Tailscale no podrás abrir el panel ni encender el PC desde fuera de casa.', action: 'Ver acceso remoto', page: 'remote' });

  const errs = alerts.filter((a) => a.sev === 'err').length;
  const warns = alerts.length - errs;
  const level = errs ? 'err' : warns ? 'warn' : 'ok';
  const pcWord = { off: 'apagado', on: 'encendido', waking: 'encendiéndose', unknown: 'en estado desconocido', notConfigured: 'sin configurar' }[pcState];
  const title = level === 'ok' ? 'Todo funciona con normalidad'
    : level === 'warn' ? (warns === 1 ? 'Todo funciona, con un aviso pendiente' : 'Todo funciona, con ' + warns + ' avisos pendientes')
    : errs === 1 ? 'Hay un problema que requiere atención' : 'Hay ' + errs + ' problemas que requieren atención';
  const detail = srvOn
    ? 'Servidor en línea, ' + (s.docker.available ? running + ' de ' + ctrs.length + ' servicios en ejecución' : 'Docker sin respuesta') + ' y PC ' + pcWord + '.'
    : 'No llegan datos nuevos del servidor. El estado de los servicios y del PC es desconocido.';

  return { s, srvOn, lastKnown, dockerOk, pcState, ctrs, running, nc, ncState, ncAppDetail, ncUsable, ncUrl, hasNC: !!nc || s.nextcloud.configured, alerts, level, title, detail, netdata, tsOk,
    diskMax: worst, memPct, updatesAvail, serverBlocked: srvOn ? '' : 'No disponible: el servidor no responde.' };
}
