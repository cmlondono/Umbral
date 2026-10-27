/* Contrato entre el panel y el servicio de Umbral en el servidor.
 * Los structs de Go en server/internal/api/types.go tienen la misma forma. */

export type Range = '1h' | '24h' | '7d';
/** Periodo de las gráficas: un historial guardado o la vista en vivo. */
export type View = Range | 'live';

export interface Session { user: string; totp?: boolean; recoveryLeft?: number }

export interface AuthStatus { configured: boolean }

export interface Disk { mount: string; label: string; device: string; totalGB: number; usedGB: number }

export interface ServerInfo {
  hostname: string;
  os: string;
  kernel: string;
  cpuModel: string;
  cores: number;
  uptimeSec: number;
  load: [number, number, number];
  ip: string;
}

export interface Metrics {
  cpuPct: number;
  mem: { totalMB: number; usedMB: number; cacheMB: number; freeMB: number };
  disks: Disk[];
  net: { rxMbps: number; txMbps: number; iface: string };
  tempC: number | null;
  tempOf: string;
}

export interface Container {
  id: string;
  name: string;
  image: string;
  state: 'running' | 'stopped' | 'restarting' | 'paused' | 'created' | 'dead';
  status: string;
  health: 'healthy' | 'unhealthy' | 'starting' | 'none';
  ports: string[];
  cpuPct: number | null;
  memMB: number | null;
  startedAt: string | null;
  url: string | null;
  compose: string | null;
}

export interface Image {
  id: string;
  repo: string;
  tag: string;
  sizeMB: number;
  created: string;
  usedBy: string[];
  exposedPort: number | null;
  volumes: string[];
  arch: string;
}

export interface Docker { available: boolean; error: string | null; version: string; containers: Container[]; images: Image[] }

export interface Nextcloud {
  configured: boolean;
  url: string;
  container: string;
  reachable: boolean;
  httpStatus: number | null;
  version: string | null;
  maintenance: boolean | null;
  error: string | null;
}

export interface PcState {
  configured: boolean;
  name: string;
  ip: string;
  mac: string;
  state: 'on' | 'off' | 'waking' | 'unknown';
  lastSeen: string | null;
  lastWake: string | null;
}

export interface TailscalePeer { name: string; ip: string; os: string; online: boolean; lastSeen: string | null }

export interface Tailscale {
  installed: boolean;
  running: boolean;
  error: string | null;
  self: { name: string; ip: string; dnsName: string } | null;
  peers: TailscalePeer[];
}

export interface NetDevice {
  ip: string;
  mac: string;
  name: string;
  custom: string;
  pinned: boolean;
  vendor: string;
  online: boolean;
  firstSeen: string;
  lastSeen: string;
  registered: 'server' | 'pc' | null;
}

export interface Network { subnet: string; gateway: string; iface: string; lastScan: string | null; devices: NetDevice[] }

export interface Update {
  container: string;
  image: string;
  state: 'available' | 'updating' | 'updated' | 'reverting' | 'failed';
  checkedAt: string;
  previousImageId: string | null;
  error: string | null;
}

export interface Updates { lastCheck: string | null; checking: boolean; items: Update[] }

export interface HistoryItem { at: string; target: string; action: string; result: 'ok' | 'error'; detail: string; user: string }

export interface Trigger {
  kind: 'schedule' | 'container' | 'app' | 'metric' | 'presence' | 'pc' | 'hook';
  days?: number[];
  time?: string;
  container?: string;
  event?: string;
  metric?: 'cpu' | 'mem' | 'disk' | 'temp';
  above?: number;
  forMin?: number;
  mac?: string;
}

export interface Condition {
  kind: 'time' | 'days' | 'device' | 'pc' | 'container';
  from?: string;
  to?: string;
  days?: number[];
  mac?: string;
  present?: boolean;
  pc?: 'on' | 'off';
  container?: string;
  running?: boolean;
}

export interface Step {
  kind: 'wake' | 'container' | 'backup' | 'scan' | 'wait' | 'http' | 'updates';
  container?: string;
  op?: 'start' | 'stop' | 'restart';
  waitOn?: boolean;
  minutes?: number;
  url?: string;
  method?: 'GET' | 'POST';
}

export interface Rule {
  id: string;
  name: string;
  enabled: boolean;
  trigger: Trigger;
  conditions: Condition[];
  steps: Step[];
  cooldownMin: number;
  created?: string;
  lastRun?: { at: string; result: 'ok' | 'error' | 'skipped'; detail: string } | null;
  hasHook?: boolean;
  armed?: 'waiting' | 'active' | 'done';
}

export interface RunStep { label: string; result: 'ok' | 'error' | 'running' | 'pending'; detail: string; ms: number }
export interface Run { id: string; ruleId: string; name: string; at: string; cause: string; result: 'running' | 'ok' | 'error' | 'skipped'; detail: string; steps: RunStep[] | null }
export interface HookURLs { local?: string; tailscale?: string }

export interface Job {
  id: string;
  kind: 'pull' | 'deploy' | 'update' | 'revert';
  target: string;
  state: 'running' | 'done' | 'error';
  progress: number;
  steps: { label: string; state: 'pending' | 'running' | 'done' | 'error' }[];
  message: string;
  result: Record<string, string> | null;
}

export interface RegisteredDevice { id: 'server' | 'pc'; name: string; ip: string; mac: string }

export interface Config {
  serverName: string;
  pc: { name: string; ip: string; mac: string; broadcast: string; checkPort: number };
  nextcloud: { url: string; container: string };
  remoteTool: 'rdp' | 'rustdesk' | 'moonlight' | null;
  sshUser: string;
  dataRoot: string;
  scanMinutes: number;
  router: { ip: string; model: string };
}

export interface Snapshot {
  at: string;
  server: ServerInfo;
  metrics: Metrics | null;
  metricsError: string | null;
  docker: Docker;
  nextcloud: Nextcloud;
  pc: PcState;
  tailscale: Tailscale;
  network: Network;
  updates: Updates;
  history: HistoryItem[];
  automations: Rule[];
  runs: Run[];
  config: Config;
  jobs: Job[];
  internet: boolean | null;
  panel: { version: string; memMB: number; uptimeSec: number; dataDir: string };
}

export interface LivePoint { at: string; cpu: number; rx: number; tx: number; mem: number; temp: number | null }

export interface History { range: Range; stepSec: number; times: string[]; cpu: number[]; rx: number[]; tx: number[]; mem: number[]; temp: number[] }

export interface CatalogApp {
  id: string;
  label: string;
  desc: string;
  image: string;
  port: number;
  cport: number;
  extraPorts: string[];
  memMB: number;
  icon: string;
  env: { k: string; v?: string; secret?: boolean }[];
  dirs: { name: string; path: string }[];
  note?: string;
  heavy?: boolean;
}

export interface DeploySpec {
  source: 'catalog' | 'image' | 'compose';
  app?: string;
  image?: string;
  compose?: string;
  name: string;
  port: number;
  cport: number;
  extraPorts: string[];
  access: 'lan' | 'tailscale';
  restart: 'always' | 'never';
  memLimitMB: number | null;
  env: { k: string; v?: string; secret?: boolean }[];
  dirs: { name: string; path: string }[];
}

export interface ApiError { error: string }
