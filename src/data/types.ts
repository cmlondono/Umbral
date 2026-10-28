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
  serve: ServeEntry[];
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
  blocked?: boolean;
}

export interface Network { subnet: string; gateway: string; iface: string; lastScan: string | null; devices: NetDevice[] }

export interface Update {
  container: string;
  image: string;
  state: 'available' | 'major' | 'updating' | 'updated' | 'reverting' | 'failed';
  checkedAt: string;
  previousImageId: string | null;
  error: string | null;
  nextMajor?: string;
}

export interface Updates { lastCheck: string | null; checking: boolean; items: Update[] }

export interface HistoryItem { at: string; target: string; action: string; result: 'ok' | 'error'; detail: string; user: string }

export interface Trigger {
  kind: 'schedule' | 'container' | 'app' | 'metric' | 'presence' | 'pc' | 'hook' | 'access';
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
  kind: 'wake' | 'container' | 'backup' | 'scan' | 'wait' | 'http' | 'updates' | 'notify' | 'copy-backups' | 'nc-full';
  container?: string;
  op?: 'start' | 'stop' | 'restart';
  waitOn?: boolean;
  minutes?: number;
  url?: string;
  method?: 'GET' | 'POST';
  text?: string;
  priority?: number;
}

export interface Rule {
  id: string;
  name: string;
  enabled: boolean;
  trigger: Trigger;
  conditions: Condition[];
  steps: Step[];
  cooldownMin: number;
  notifyFail?: boolean;
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
  router: RouterConfig;
  backupCopyDir: string;
  ncBackupKeep: number;
  ncBackupMaint: boolean;
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
  panel: { version: string; memMB: number; uptimeSec: number; dataDir: string; port: number };
  notify: NotifyView;
  avail: AvailView[];
  incidents: Incident[];
  system: SystemState;
  router: RouterState;
}

export interface RouterConfig { kind: '' | 'openwrt' | 'mikrotik'; url: string; user: string; insecure: boolean }
export interface RouterInfo { model: string; firmware: string; uptimeSec: number; hostname: string }
export interface RouterState { configured: boolean; kind: string; connected: boolean; error: string; info: RouterInfo | null; blocked: string[]; leases: number; checkedAt: string | null; hasPassword: boolean }
export interface ServeEntry { port: number; target: string; url: string; local: number }

export interface SmartDisk { name: string; model: string; serial: string; sizeGB: number; kind: 'nvme' | 'ssd' | 'hdd' | ''; supported: boolean; passed: boolean | null; tempC: number | null; hours: number | null; reallocated: number | null; pending: number | null; uncorrectable: number | null; percentUsed: number | null; mediaErrors: number | null; health: 'ok' | 'warn' | 'fail' | 'unknown'; note: string }
export interface AptPkg { name: string; from: string; to: string; security: boolean }
export interface SystemState {
  helperInstalled: boolean; requested: string | null;
  smart: { at: string; available: boolean; disks: SmartDisk[] } | null;
  aptAvailable: boolean; aptCheckedAt: string | null; aptError: string; listsAt: string | null;
  listsUpdate: { at: string; ok: boolean; error: string } | null;
  packages: AptPkg[]; security: number; reboot: boolean; rebootPkgs: string[] | null; unattended: boolean;
}
export interface BackupFile { name: string; db: string; sizeKB: number; at: string; copied: boolean }
export interface CopyDirStatus { path: string; ok: boolean; problem: string; freeGB: number; sameDisk: boolean; files: number; lastCopy: string }
export interface FileSnapshot { name: string; at: string; ok: boolean; error: string; files: number; transferredMB: number; totalMB: number; seconds: number; maintenance: boolean; exists: boolean }
export interface Backups { files: BackupFile[]; copyDir: CopyDirStatus | null; ncFiles?: FileSnapshot[] }

export interface NotifyView { configured: boolean; server: string; topic: string; hasToken: boolean }
export interface AvailView { target: string; pct24h: number | null; pct7d: number | null; hours: number; down: string | null }
export interface Incident { target: string; from: string; to: string | null }
export interface AccessEvent { at: string; user: string; ip: string; kind: 'login' | 'fail' | 'locked' | 'logout' | 'password' | 'totp-on' | 'totp-off'; agent: string; new: boolean }

export interface LogLine { at: string; text: string; err: boolean }
export interface ContainerDetails { memLimitMB: number; restart: string; createdAt: string; mounts: { type: string; name: string; source: string; destination: string }[] | null }
export interface DockerVolume { name: string; sizeMB: number; createdAt: string; stack: string; anonymous: boolean }
export interface DockerDisk { buildCacheMB: number; unused: DockerVolume[]; volumesMB: number; imagesMB: number }

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
