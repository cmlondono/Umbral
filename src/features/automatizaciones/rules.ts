import type { Condition, Rule, Snapshot, Step, Trigger } from '../../data/types';
import type { IconName } from '../../design/ui';

/* Lenguaje común de las automatizaciones: frases legibles y plantillas. */

export const DAY_SHORT = ['D', 'L', 'M', 'X', 'J', 'V', 'S'];
export const DAY_NAMES = ['domingo', 'lunes', 'martes', 'miércoles', 'jueves', 'viernes', 'sábado'];
export const WEEK = [1, 2, 3, 4, 5, 6, 0];

export function daysText(days: number[] = []) {
  const ds = [...days].sort().join();
  if (ds === '1,2,3,4,5') return 'de lunes a viernes';
  if (ds === '0,1,2,3,4,5,6') return 'todos los días';
  if (ds === '0,6') return 'los fines de semana';
  return WEEK.filter((d) => days.includes(d)).map((d) => DAY_NAMES[d]).join(', ');
}

const METRIC: Record<string, [string, string]> = { cpu: ['el procesador', '%'], mem: ['la memoria', '%'], disk: ['el disco', '%'], temp: ['la temperatura', '°C'] };

export function deviceName(s: Snapshot | null, mac?: string) {
  const d = s?.network.devices.find((x) => x.mac.toUpperCase() === (mac || '').toUpperCase());
  return d?.name || mac || 'un dispositivo';
}

export const TRIGGERS: { kind: Trigger['kind']; label: string; icon: IconName; hint: string }[] = [
  { kind: 'schedule', label: 'A una hora', icon: 'clock', hint: 'Días y hora concretos' },
  { kind: 'hook', label: 'Atajo o NFC', icon: 'nfc', hint: 'Desde el iPhone o una etiqueta' },
  { kind: 'container', label: 'Un servicio cambia', icon: 'boxes', hint: 'Se detiene, falla o arranca' },
  { kind: 'app', label: 'Nextcloud no responde', icon: 'cloud', hint: 'Durante varios minutos' },
  { kind: 'metric', label: 'Un recurso se dispara', icon: 'activity', hint: 'CPU, memoria, disco o temperatura' },
  { kind: 'presence', label: 'Llega o se va alguien', icon: 'phone', hint: 'Un dispositivo entra o sale de la red' },
  { kind: 'pc', label: 'El PC cambia', icon: 'monitor', hint: 'Se enciende o se apaga' },
];

export const STEPS: { kind: Step['kind']; label: string; icon: IconName }[] = [
  { kind: 'wake', label: 'Encender el PC', icon: 'power' },
  { kind: 'container', label: 'Iniciar, detener o reiniciar un servicio', icon: 'boxes' },
  { kind: 'backup', label: 'Copia de la base de datos de Nextcloud', icon: 'download' },
  { kind: 'wait', label: 'Esperar', icon: 'clock' },
  { kind: 'scan', label: 'Escanear la red', icon: 'radar' },
  { kind: 'updates', label: 'Buscar actualizaciones', icon: 'refresh' },
  { kind: 'http', label: 'Llamar a una dirección de tu red', icon: 'external' },
];

export const CONDITIONS: { kind: Condition['kind']; label: string }[] = [
  { kind: 'time', label: 'Franja horaria' },
  { kind: 'days', label: 'Días' },
  { kind: 'device', label: 'Alguien está en casa' },
  { kind: 'pc', label: 'Estado del PC' },
  { kind: 'container', label: 'Estado de un servicio' },
];

export function triggerText(t: Trigger, s: Snapshot | null) {
  const dur = t.forMin ? ' durante ' + t.forMin + ' min' : '';
  switch (t.kind) {
    case 'schedule': { const dt = daysText(t.days) || 'ningún día'; return dt.charAt(0).toUpperCase() + dt.slice(1) + ' a las ' + (t.time || '—').replace(/^0/, ''); }
    case 'hook': return 'Al abrir su enlace (Atajo o NFC)';
    case 'container': return 'Cuando ' + (t.container || 'un servicio') + ' ' + ({ stopped: 'se detiene', unhealthy: 'falla', running: 'se pone en marcha' } as Record<string, string>)[t.event || 'stopped'] + dur;
    case 'app': return 'Cuando Nextcloud no responde durante ' + (t.forMin || 3) + ' min';
    case 'metric': { const [w, u] = METRIC[t.metric || 'cpu']; return 'Cuando ' + w + ' supera ' + (t.above ?? '—') + ' ' + u + dur; }
    case 'presence': return 'Cuando ' + deviceName(s, t.mac) + (t.event === 'leave' ? ' se va de casa' : ' llega a casa');
    case 'pc': return 'Cuando el PC se ' + (t.event === 'off' ? 'apaga' : 'enciende');
  }
}

export function conditionText(c: Condition, s: Snapshot | null) {
  switch (c.kind) {
    case 'time': return 'entre las ' + (c.from || '—') + ' y las ' + (c.to || '—');
    case 'days': return daysText(c.days);
    case 'device': return deviceName(s, c.mac) + (c.present ? ' está en casa' : ' no está en casa');
    case 'pc': return 'el PC está ' + (c.pc === 'off' ? 'apagado' : 'encendido');
    case 'container': return (c.container || 'un servicio') + (c.running ? ' está en marcha' : ' está detenido');
  }
}

export function stepText(st: Step) {
  switch (st.kind) {
    case 'wake': return st.waitOn ? 'encender el PC y esperar a que responda' : 'encender el PC';
    case 'container': return ({ start: 'iniciar ', stop: 'detener ', restart: 'reiniciar ' } as Record<string, string>)[st.op || 'restart'] + (st.container || 'un servicio');
    case 'backup': return 'copiar la base de datos de Nextcloud';
    case 'wait': return 'esperar ' + (st.minutes || 1) + ' min';
    case 'scan': return 'escanear la red';
    case 'updates': return 'buscar actualizaciones';
    case 'http': return 'llamar a ' + (st.url || 'una dirección');
  }
}

export function sentence(r: Rule, s: Snapshot | null) {
  const cond = r.conditions.length ? ', si ' + r.conditions.map((c) => conditionText(c, s)).join(' y ') : '';
  const steps = r.steps.map(stepText);
  const acts = steps.length > 1 ? steps.slice(0, -1).join(', ') + ' y ' + steps[steps.length - 1] : steps[0] || '…';
  return triggerText(r.trigger, s) + cond + ' → ' + acts + '.';
}

export function triggerIcon(t: Trigger): IconName {
  return TRIGGERS.find((x) => x.kind === t.kind)?.icon || 'workflow';
}

export const blank = (): Rule => ({ id: '', name: '', enabled: true, trigger: { kind: 'schedule', days: [1, 2, 3, 4, 5], time: '07:30' }, conditions: [], steps: [], cooldownMin: 10 });

/* Plantillas: solo las que funcionan con lo que tiene el servidor. */
export function templates(s: Snapshot): { id: string; title: string; desc: string; icon: IconName; rule: Rule; needs?: string }[] {
  const nc = s.nextcloud.container || 'nextcloud-app';
  const phone = s.network.devices.find((d) => /iphone/i.test(d.name))?.mac || '';
  const pcOk = s.pc.configured;
  const hasNC = s.docker.containers.some((c) => c.name === s.nextcloud.container) || s.nextcloud.configured;
  let list: { id: string; title: string; desc: string; icon: IconName; rule: Rule; needs?: string }[] = [
    { id: 'nc-restart', title: 'Reiniciar Nextcloud si deja de responder', desc: 'Si no responde 5 minutos seguidos, reinicia su contenedor. Como mucho una vez cada 30 min.', icon: 'cloud',
      rule: { ...blank(), name: 'Reiniciar Nextcloud si deja de responder', trigger: { kind: 'app', forMin: 5 }, steps: [{ kind: 'container', container: nc, op: 'restart' }], cooldownMin: 30 } },
    { id: 'nc-backup', title: 'Copia nocturna de Nextcloud', desc: 'Cada día a las 3:00 copia la base de datos. Se guardan las 5 más recientes.', icon: 'download',
      rule: { ...blank(), name: 'Copia nocturna de Nextcloud', trigger: { kind: 'schedule', days: [0, 1, 2, 3, 4, 5, 6], time: '03:00' }, steps: [{ kind: 'backup' }], cooldownMin: 0 } },
    { id: 'svc-heal', title: 'Volver a arrancar un servicio que se cae', desc: 'Si un servicio se detiene durante 2 minutos, lo inicia otra vez.', icon: 'boxes',
      rule: { ...blank(), name: 'Volver a arrancar un servicio', trigger: { kind: 'container', container: s.docker.containers[0]?.name || '', event: 'stopped', forMin: 2 }, steps: [{ kind: 'container', container: s.docker.containers[0]?.name || '', op: 'start' }], cooldownMin: 15 } },
    { id: 'pc-nfc', title: 'Encender el PC con el iPhone o una etiqueta NFC', desc: 'El panel te da un enlace para Atajos. Lo tocas y el PC se enciende.', icon: 'nfc', needs: pcOk ? undefined : 'Necesita el PC configurado.',
      rule: { ...blank(), name: 'Encender el PC desde el iPhone', trigger: { kind: 'hook' }, steps: [{ kind: 'wake', waitOn: true }], cooldownMin: 0 } },
    { id: 'pc-home', title: 'Encender el PC al llegar a casa', desc: 'Cuando tu iPhone se conecta al Wi‑Fi de casa, entre las 6:00 y las 22:00.', icon: 'phone', needs: !pcOk ? 'Necesita el PC configurado.' : !phone ? 'Necesita ver tu iPhone en Red.' : undefined,
      rule: { ...blank(), name: 'Encender el PC al llegar a casa', trigger: { kind: 'presence', mac: phone, event: 'arrive' }, conditions: [{ kind: 'time', from: '06:00', to: '22:00' }, { kind: 'pc', pc: 'off' }], steps: [{ kind: 'wake', waitOn: true }], cooldownMin: 60 } },
    { id: 'pc-work', title: 'Encender el PC los días laborables', desc: 'De lunes a viernes a las 7:30, y comprueba que responde.', icon: 'clock', needs: pcOk ? undefined : 'Necesita el PC configurado.',
      rule: { ...blank(), name: 'Encender el PC los días laborables', trigger: { kind: 'schedule', days: [1, 2, 3, 4, 5], time: '07:30' }, steps: [{ kind: 'wake', waitOn: true }], cooldownMin: 0 } },
  ];
  if (!hasNC) list = list.filter((t) => !t.id.startsWith('nc-'));
  if (s.metrics?.tempC != null) list.push({ id: 'hot', title: 'Aliviar el servidor si se calienta', desc: 'Si la temperatura pasa de 85 °C durante 5 min, detiene el servicio que elijas.', icon: 'activity',
    rule: { ...blank(), name: 'Aliviar el servidor si se calienta', trigger: { kind: 'metric', metric: 'temp', above: 85, forMin: 5 }, steps: [{ kind: 'container', container: '', op: 'stop' }], cooldownMin: 60 } });
  return list;
}
