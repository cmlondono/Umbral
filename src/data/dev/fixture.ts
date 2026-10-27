/* SOLO PARA DESARROLLO. Servidor simulado en memoria para revisar el panel
 * sin el servidor real. Se activa con VITE_UMBRAL_PRUEBAS=1 en `npm run dev`
 * y nunca entra en la compilación que se instala.
 * Parámetros de la URL para probar estados: ?mantenimiento, ?sinservidor. */
import { ApiError, type Adapter } from '../adapters';
import type { CatalogApp, Container, History, Job, Range, Snapshot } from '../types';

const q = new URLSearchParams(location.search);
const now = () => new Date().toISOString();
const minsAgo = (m: number) => new Date(Date.now() - m * 60000).toISOString();
const wait = (ms: number) => new Promise((r) => setTimeout(r, ms));
let logged = sessionStorage.getItem('umbral.pruebas.sesion') === '1';
let calls = 0;

const ctr = (o: Partial<Container> & Pick<Container, 'name' | 'image'>): Container => ({ id: o.name + '0'.repeat(12), state: 'running', status: 'Up 6 days', health: 'none', ports: [], cpuPct: 0.5, memMB: 40, startedAt: null, url: null, compose: null, ...o });

const snap: Snapshot = {
  at: now(),
  server: { hostname: 'servidor-casa', os: 'Debian GNU/Linux 12 (bookworm)', kernel: '6.1.0-26-amd64', cpuModel: 'AMD Ryzen 5 3400G', cores: 8, uptimeSec: 23 * 86400 + 4 * 3600, load: [0.42, 0.51, 0.48], ip: '192.168.1.10' },
  metrics: { cpuPct: 9.4, mem: { totalMB: 7790, usedMB: 2310, cacheMB: 3420, freeMB: 2060 }, disks: [{ mount: '/', label: 'Sistema', device: 'nvme0n1p2 · SSD', totalGB: 234, usedGB: 41.3 }, { mount: '/srv', label: 'Datos', device: 'sda1 · HDD', totalGB: 931, usedGB: 512.4 }], net: { rxMbps: 2.4, txMbps: 0.6, iface: 'enp3s0' }, tempC: 47.5, tempOf: 'k10temp' },
  metricsError: null,
  docker: {
    available: true, error: null, version: '27.3.1',
    containers: [
      ctr({ name: 'nextcloud', image: 'nextcloud:30-apache', ports: ['8080→80/tcp'], cpuPct: 1.8, memMB: 312, url: 'http://192.168.1.10:8080', compose: 'nextcloud' }),
      ctr({ name: 'nextcloud-db', image: 'mariadb:11.4', status: 'Up 6 days (healthy)', health: 'healthy', cpuPct: 0.7, memMB: 168, compose: 'nextcloud' }),
      ctr({ name: 'netdata', image: 'netdata/netdata:stable', ports: ['19999→19999/tcp'], status: 'Up 23 days (healthy)', health: 'healthy', cpuPct: 3.1, memMB: 214, url: 'http://192.168.1.10:19999' }),
    ],
    images: [
      { id: '3f1c9a2b7d4e', repo: 'nextcloud', tag: '30-apache', sizeMB: 1270, created: minsAgo(15 * 1440), usedBy: ['nextcloud'], exposedPort: 80, volumes: ['/var/www/html'], arch: 'amd64' },
      { id: '9b2e0c41a7f3', repo: 'mariadb', tag: '11.4', sizeMB: 405, created: minsAgo(30 * 1440), usedBy: ['nextcloud-db'], exposedPort: 3306, volumes: ['/var/lib/mysql'], arch: 'amd64' },
      { id: 'a4c39e8d1f27', repo: 'netdata/netdata', tag: 'stable', sizeMB: 640, created: minsAgo(40 * 1440), usedBy: ['netdata'], exposedPort: 19999, volumes: [], arch: 'amd64' },
      { id: '1e8d4c7b2a95', repo: '<none>', tag: '<none>', sizeMB: 1260, created: minsAgo(45 * 1440), usedBy: [], exposedPort: 80, volumes: ['/var/www/html'], arch: 'amd64' },
    ],
  },
  nextcloud: { configured: true, url: 'http://192.168.1.10:8080', container: 'nextcloud', reachable: true, httpStatus: 200, version: '30.0.2', maintenance: false, error: null },
  pc: { configured: false, name: '', ip: '', mac: '', state: 'unknown', lastSeen: null, lastWake: null },
  tailscale: { installed: true, running: true, error: null, self: { name: 'servidor-casa', ip: '100.101.12.4', dnsName: 'servidor-casa.tail3f2a1.ts.net' },
    peers: [{ name: 'iphone', ip: '100.88.40.2', os: 'iOS', online: true, lastSeen: null }, { name: 'portatil', ip: '100.77.3.9', os: 'windows', online: false, lastSeen: minsAgo(300) }] },
  network: { subnet: '192.168.1.0/24', gateway: '192.168.1.1', iface: 'enp3s0', lastScan: minsAgo(2), devices: [
    { ip: '192.168.1.1', mac: '74:DA:88:10:3E:01', name: 'router', custom: '', pinned: false, vendor: '', online: true, firstSeen: minsAgo(9000), lastSeen: now(), registered: null },
    { ip: '192.168.1.10', mac: 'B8:AE:ED:4C:21:9F', name: 'servidor-casa', custom: '', pinned: false, vendor: '', online: true, firstSeen: minsAgo(9000), lastSeen: now(), registered: 'server' },
    { ip: '192.168.1.20', mac: '3C:7C:3F:A1:52:0E', name: 'ESCRITORIO-PC', custom: '', pinned: false, vendor: '', online: false, firstSeen: minsAgo(9000), lastSeen: minsAgo(1028), registered: null },
    { ip: '192.168.1.34', mac: 'DA:A1:19:5E:22:B0', name: 'iPhone', custom: '', pinned: false, vendor: 'MAC privada (aleatoria)', online: true, firstSeen: minsAgo(9000), lastSeen: now(), registered: null },
    { ip: '192.168.1.52', mac: '8C:79:F5:20:AA:61', name: 'LGwebOSTV', custom: '', pinned: false, vendor: '', online: true, firstSeen: minsAgo(9000), lastSeen: now(), registered: null },
    { ip: '192.168.1.57', mac: '44:00:49:9B:31:C7', name: 'echo-dot', custom: '', pinned: false, vendor: '', online: true, firstSeen: minsAgo(9000), lastSeen: now(), registered: null },
    { ip: '192.168.1.73', mac: '5E:22:B0:DA:A1:19', name: '', custom: '', pinned: false, vendor: 'MAC privada (aleatoria)', online: true, firstSeen: minsAgo(47), lastSeen: now(), registered: null },
  ] },
  updates: { lastCheck: minsAgo(300), checking: false, items: [{ container: 'nextcloud', image: 'nextcloud:30-apache', state: 'available', checkedAt: minsAgo(300), previousImageId: null, error: null }] },
  history: [{ at: minsAgo(6 * 1440), target: 'nextcloud', action: 'Reinicio', result: 'ok', detail: '', user: 'usuario' }],
  automations: [
    { id: 'r1', name: 'Encender el PC los días laborables', enabled: false, trigger: { kind: 'schedule', days: [1, 2, 3, 4, 5], time: '07:30' }, conditions: [], steps: [{ kind: 'wake', waitOn: true }], cooldownMin: 0, lastRun: null, hasHook: false, armed: 'waiting' },
  ],
  runs: [],
  config: { serverName: 'servidor-casa', pc: { name: '', ip: '', mac: '', broadcast: '', checkPort: 3389 }, nextcloud: { url: '', container: 'nextcloud' }, remoteTool: null, sshUser: '', dataRoot: '/srv/umbral', scanMinutes: 5, router: { ip: '', model: '' } },
  jobs: [], internet: true, panel: { version: '0.1.0', memMB: 14, uptimeSec: 3600 * 5, dataDir: '/var/lib/umbral' },
};
if (q.has('mantenimiento')) Object.assign(snap.nextcloud, { httpStatus: 503, maintenance: true });

const jobs = new Map<string, Job>();
function startJob(kind: Job['kind'], target: string, labels: string[], onDone: () => void, msg: string): Job {
  const j: Job = { id: Math.random().toString(36).slice(2, 10), kind, target, state: 'running', progress: 0, steps: labels.map((label, i) => ({ label, state: i ? 'pending' : 'running' })), message: '', result: null };
  jobs.set(j.id, j);
  let i = 0;
  const t = setInterval(() => {
    j.progress = Math.min(100, j.progress + 17);
    if (j.progress >= ((i + 1) / labels.length) * 100) { j.steps[i].state = 'done'; i++; if (j.steps[i]) j.steps[i].state = 'running'; }
    if (i >= labels.length) { clearInterval(t); j.state = 'done'; j.progress = 100; j.message = msg; onDone(); }
  }, 450);
  return j;
}

async function guard<T>(fn: () => T): Promise<T> {
  await wait(220);
  calls++;
  if (q.has('sinservidor') && calls > 2) throw new ApiError('El servidor no responde.', 0);
  if (!logged) throw new ApiError('La sesión ha caducado.', 401);
  snap.at = now();
  return structuredClone(fn());
}
const S = () => snap;

const series = (r: Range): History => {
  const n = { '1h': 60, '24h': 96, '7d': 84 }[r], step = { '1h': 60, '24h': 900, '7d': 7200 }[r];
  const times: string[] = [], cpu: number[] = [], rx: number[] = [], tx: number[] = [], mem: number[] = [], temp: number[] = [];
  let a = 7;
  const rnd = () => { a = (a * 9301 + 49297) % 233280; return a / 233280; };
  for (let i = 0; i < n; i++) {
    const t = new Date(Date.now() - (n - 1 - i) * step * 1000), h = t.getHours();
    times.push(t.toISOString());
    cpu.push(5 + rnd() * 8 + (h === 3 ? 35 : 0));
    rx.push(0.8 + rnd() * 2.6 + (h >= 19 && h < 23 ? 8 + rnd() * 10 : 0));
    tx.push(0.2 + rnd() * 0.8);
    mem.push(28 + rnd() * 4 + (h >= 19 && h < 23 ? 3 : 0));
    temp.push(42 + rnd() * 4 + (h === 3 ? 14 : 0));
  }
  return { range: r, stepSec: step, times, cpu, rx, tx, mem, temp };
};

const catalog: CatalogApp[] = [
  { id: 'vaultwarden', label: 'Vaultwarden', desc: 'Gestor de contraseñas compatible con Bitwarden', image: 'vaultwarden/server:latest', port: 8081, cport: 80, extraPorts: [], memMB: 40, icon: 'lock', env: [{ k: 'ADMIN_TOKEN', secret: true }, { k: 'SIGNUPS_ALLOWED', v: 'false' }], dirs: [{ name: 'data', path: '/data' }], note: 'Las apps de Bitwarden exigen HTTPS: publícalo con Tailscale.' },
  { id: 'adguard', label: 'AdGuard Home', desc: 'Bloquea anuncios y rastreadores en toda la red', image: 'adguard/adguardhome:latest', port: 3000, cport: 3000, extraPorts: ['53→53/udp', '53→53/tcp'], memMB: 60, icon: 'shield', env: [], dirs: [{ name: 'work', path: '/opt/adguardhome/work' }, { name: 'conf', path: '/opt/adguardhome/conf' }], note: 'Para filtrar toda la red, el router tiene que usar este servidor como DNS. Ese cambio se hace en el router.' },
  { id: 'ntfy', label: 'ntfy', desc: 'Notificaciones en el iPhone desde tu servidor', image: 'binwiederhier/ntfy:latest', port: 8082, cport: 80, extraPorts: [], memMB: 20, icon: 'bell', env: [{ k: 'TZ', v: 'UTC' }], dirs: [{ name: 'cache', path: '/var/cache/ntfy' }] },
  { id: 'jellyfin', label: 'Jellyfin', desc: 'Películas, series y música en tus dispositivos', image: 'jellyfin/jellyfin:latest', port: 8096, cport: 8096, extraPorts: [], memMB: 350, icon: 'tv', env: [{ k: 'TZ', v: 'UTC' }], dirs: [{ name: 'config', path: '/config' }, { name: 'cache', path: '/cache' }] },
  { id: 'homeassistant', label: 'Home Assistant', desc: 'Domótica: controla y automatiza los dispositivos del hogar', image: 'ghcr.io/home-assistant/home-assistant:stable', port: 8123, cport: 8123, extraPorts: [], memMB: 420, icon: 'home', env: [{ k: 'TZ', v: 'UTC' }], dirs: [{ name: 'config', path: '/config' }], heavy: true },
];

export const fixtureAdapter: Adapter = {
  authStatus: async () => ({ configured: true }),
  session: async () => { await wait(150); if (!logged) throw new ApiError('Sin sesión', 401); return { user: 'usuario' }; },
  login: async (user, password) => {
    await wait(500);
    if (!user || password.length < 4) throw new ApiError('Usuario o contraseña incorrectos.', 401);
    logged = true; sessionStorage.setItem('umbral.pruebas.sesion', '1');
    return { user: user.toLowerCase() };
  },
  changePassword: async (current, next) => {
    await wait(400);
    if (current.length < 4) throw new ApiError('La contraseña actual no es correcta.', 400);
    if (next.length < 10) throw new ApiError('La contraseña nueva debe tener al menos 10 caracteres.', 400);
    return { user: 'usuario' };
  },
  totpBegin: async (p) => { await wait(300); if (p.length < 4) throw new ApiError('La contraseña no es correcta.', 400); return { secret: 'JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP', uri: 'otpauth://totp/Umbral%20(servidor-casa):usuario?secret=JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP&issuer=Umbral' }; },
  totpConfirm: async (c) => { await wait(300); if (c.length !== 6) throw new ApiError('El código no coincide.', 400); return { recovery: ['abcd-efgh', 'jkmn-pqrs', 'tuvw-xyz2', '3456-789a', 'bcde-fghj', 'kmnp-qrst', 'uvwx-yz23', '4567-89ab'] }; },
  totpDisable: async () => { await wait(300); return { ok: true }; },
  logout: async () => { logged = false; sessionStorage.removeItem('umbral.pruebas.sesion'); },
  state: () => guard(S),
  refresh: () => guard(S),
  history: (r) => guard(() => series(r)),
  live: () => guard(() => ({ at: new Date().toISOString(), cpu: 6 + Math.random() * 14, rx: 0.5 + Math.random() * 6, tx: 0.2 + Math.random(), mem: 30 + Math.random() * 2, temp: 44 + Math.random() * 5 })),
  catalog: () => guard(() => catalog),
  job: (id) => guard(() => { const j = jobs.get(id); if (!j) throw new ApiError('Esa tarea ya no existe.', 404); return j; }),
  containerAction: (name, action) => guard(() => {
    const c = snap.docker.containers.find((k) => k.name === name)!;
    if (action === 'remove') { snap.docker.containers = snap.docker.containers.filter((k) => k !== c); snap.docker.images.forEach((i) => { i.usedBy = i.usedBy.filter((u) => u !== name); }); }
    else { c.state = action === 'stop' ? 'stopped' : 'running'; c.status = action === 'stop' ? 'Exited (0) hace un momento' : 'Up 1 second'; }
    snap.history.unshift({ at: now(), target: name, action: { start: 'Inicio', stop: 'Detención', restart: 'Reinicio', remove: 'Contenedor eliminado' }[action], result: 'ok', detail: '', user: 'usuario' });
    return snap;
  }),
  pullImage: (ref) => guard(() => startJob('pull', ref.includes(':') ? ref : ref + ':latest', ['Descargando ' + ref], () => {
    const [repo, tag = 'latest'] = ref.split(':');
    if (!snap.docker.images.some((i) => i.repo === repo && i.tag === tag)) snap.docker.images.push({ id: Math.random().toString(16).slice(2, 14), repo, tag, sizeMB: 120, created: now(), usedBy: [], exposedPort: 80, volumes: ['/data'], arch: 'amd64' });
  }, 'Descargada.')),
  backupDatabase: (container) => guard(() => ({ path: '/srv/umbral/copias/' + container + '-db-20261027-120000.sql.gz', sizeKB: 2310, database: container + '-db' })),
  removeImages: (items) => guard(() => { snap.docker.images = snap.docker.images.filter((i) => !items.some((x) => x.id === i.id)); return snap; }),
  deploy: (spec) => guard(() => startJob('deploy', spec.name, ['Descargando ' + (spec.image || spec.app), 'Creando /srv/umbral/' + spec.name, 'Creando el contenedor ' + spec.name, 'Comprobando que responde'], () => {
    snap.docker.containers.push(ctr({ name: spec.name, image: spec.image || catalog.find((a) => a.id === spec.app)?.image || 'imagen', ports: [spec.port + '→' + spec.cport + '/tcp'], url: 'http://192.168.1.10:' + spec.port }));
  }, spec.name + ' está en marcha y responde en http://192.168.1.10:' + spec.port + '.')),
  checkUpdates: () => guard(S),
  applyUpdate: (name) => guard(() => { const u = snap.updates.items.find((i) => i.container === name)!; u.state = 'updating'; return startJob('update', name, ['Descargando', 'Recreando', 'Comprobando'], () => { u.state = 'updated'; u.previousImageId = 'sha256:abc'; }, name + ' ya usa la versión nueva.'); }),
  revertUpdate: (name) => guard(() => { const u = snap.updates.items.find((i) => i.container === name)!; u.state = 'reverting'; return startJob('revert', name, ['Volviendo', 'Comprobando'], () => { u.state = 'available'; u.previousImageId = null; }, name + ' vuelve a usar la versión anterior.'); }),
  wakePc: () => guard(() => { snap.pc.state = 'waking'; snap.pc.lastWake = now(); setTimeout(() => { snap.pc.state = 'on'; snap.pc.lastSeen = now(); }, 5000); return snap; }),
  checkPc: () => guard(S),
  updateDevice: (mac, name, pinned) => guard(() => { const v = snap.network.devices.find((x) => x.mac === mac)!; v.custom = name; v.pinned = pinned; v.name = name || v.name; return snap; }),
  scanNetwork: () => guard(() => { snap.network.lastScan = now(); return snap; }),
  saveConfig: (c) => guard(() => {
    if (c.pc.mac && !/^([0-9A-F]{2}:){5}[0-9A-F]{2}$/i.test(c.pc.mac)) throw new ApiError('La dirección MAC del PC no es válida. Usa el formato 3C:7C:3F:A1:52:0E.', 400);
    snap.config = { ...c, pc: { ...c.pc, mac: c.pc.mac.toUpperCase() } };
    Object.assign(snap.pc, { configured: !!(c.pc.ip && c.pc.mac), name: c.pc.name, ip: c.pc.ip, mac: c.pc.mac.toUpperCase(), state: c.pc.ip ? 'off' : 'unknown', lastSeen: minsAgo(1028) });
    snap.network.devices.forEach((v) => { if (v.registered !== 'server') v.registered = v.mac === snap.pc.mac ? 'pc' : null; });
    return snap;
  }),
  saveRule: (rule) => guard(() => {
    const isNew = !rule.id;
    const r = { ...rule, id: rule.id || Math.random().toString(36).slice(2, 8), hasHook: rule.trigger.kind === 'hook', armed: 'waiting' as const };
    snap.automations = isNew ? [...snap.automations, r] : snap.automations.map((x) => (x.id === r.id ? { ...x, ...r } : x));
    return { id: r.id, hook: isNew && r.trigger.kind === 'hook' ? { local: 'http://192.168.1.10:8420/api/hooks/' + 'x'.repeat(32), tailscale: 'http://servidor-casa.tail3f2a1.ts.net:8420/api/hooks/' + 'x'.repeat(32) } : null, state: snap };
  }),
  setRuleEnabled: (id, enabled) => guard(() => { snap.automations.forEach((r) => { if (r.id === id) r.enabled = enabled; }); return snap; }),
  deleteRule: (id) => guard(() => { snap.automations = snap.automations.filter((r) => r.id !== id); return snap; }),
  runRule: (id) => guard(() => {
    const r = snap.automations.find((x) => x.id === id)!;
    const run = { id: Math.random().toString(36).slice(2, 8), ruleId: id, name: r.name, at: now(), cause: 'Ejecutada a mano', result: 'ok' as const, detail: 'Todas las acciones se completaron.', steps: r.steps.map((s) => ({ label: s.kind, result: 'ok' as const, detail: 'Hecho.', ms: 1200 })) };
    snap.runs = [run, ...snap.runs];
    r.lastRun = { at: run.at, result: 'ok', detail: run.detail };
    return snap;
  }),
  newHook: () => guard(() => ({ hook: { local: 'http://192.168.1.10:8420/api/hooks/' + 'y'.repeat(32) } })),
};
