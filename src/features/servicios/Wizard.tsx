import { useEffect, useRef } from 'react';
import type { CatalogApp, Image } from '../../data/types';
import type { Derived } from '../../state/derive';
import { adapter, follow, get, loadCatalog, set, useStore, type ParsedCompose, type WizState } from '../../state/store';
import { Badge, Icon, Seg, Sym, glyphPath, memLabel, st, type IconName } from '../../design/ui';
import { imgRef, openUrl } from '../../state/actions';

/* ---------- Apertura ---------- */
const blank = (): WizState => ({ step: 1, source: 'catalog', app: null, compose: '', parsed: null, image: '', cport: '80', name: '', port: '', access: 'lan', restart: 'always', memLimit: 'none', initKey: '', jobId: null, job: null, submitErr: null });

export function openWizard(appId?: string) {
  set({ page: 'services', moreOpen: false, drawer: null, wiz: { ...blank(), app: appId || null } });
  void loadCatalog();
}

export function openWizardForImage(im: Image) {
  set({ page: 'services', moreOpen: false, drawer: null, pull: null, wiz: { ...blank(), source: 'image', image: imgRef(im), cport: String(im.exposedPort || 80) } });
  void loadCatalog().then(() => next());
}

const setWiz = (p: Partial<WizState>) => set((s) => (s.wiz ? { wiz: { ...s.wiz, ...p } } : {}));

/* ---------- Lógica pura ---------- */
export function parseCompose(t: string): ParsedCompose {
  const images: string[] = [], ports: [string, string][] = [], svc: string[] = [];
  let m: RegExpExecArray | null, inS = false;
  const ri = /^\s*image:\s*["']?([^"'\s#]+)/gm; while ((m = ri.exec(t))) images.push(m[1]);
  const rp = /^\s*-\s*["']?(?:[\d.]+:)?(\d+):(\d+)(?:\/\w+)?["']?\s*$/gm; while ((m = rp.exec(t))) ports.push([m[1], m[2]]);
  t.split('\n').forEach((l) => {
    l = l.replace(/\r$/, '');
    if (/^services:\s*$/.test(l)) { inS = true; return; }
    if (inS && /^\S/.test(l) && !l.startsWith('#')) inS = false;
    const k = /^ {2}([A-Za-z0-9_.-]+):\s*$/.exec(l); if (inS && k) svc.push(k[1]);
  });
  if (!/^services:\s*$/m.test(t)) return { err: 'Falta la sección services:. Un archivo docker-compose empieza normalmente por esa línea.' };
  if (!images.length) return { err: 'No se encontró ninguna imagen. Cada servicio necesita una línea image: (este asistente no construye imágenes con build:).' };
  return { services: svc.length ? svc : ['servicio'], images, ports };
}

const validImage = (t: string) => /^[a-z0-9]+([._/:-][a-z0-9]+)*(:[A-Za-z0-9._-]+)?$/.test((t || '').trim());
const validCport = (t: string) => { const n = parseInt(t, 10); return String(n) === String(t).trim() && n >= 1 && n <= 65535; };
const slug = (t: string) => (t || '').toLowerCase().normalize('NFD').replace(/[̀-ͯ]/g, '').replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '').replace(/^[^a-z]+/, '').slice(0, 30) || 'servicio';
const withTag = (r: string) => { const c = r.lastIndexOf(':'), sl = r.lastIndexOf('/'); return c > sl ? r : r + ':latest'; };

interface Spec {
  id?: string; label: string; image: string; cport: number; defaultPort: number; extraPorts: string[]; mem: number | null; heavy?: boolean;
  env: CatalogApp['env']; dirs: { name: string; path: string }[]; icon: IconName; custom: boolean; compose?: boolean; nServices?: number; local?: boolean; note?: string;
}

function localImage(ref: string) { return get().snap?.docker.images.find((i) => imgRef(i) === withTag(ref.trim())); }

function spec(w: WizState): Spec | null {
  if (w.source === 'catalog') {
    const a = get().catalog?.find((k) => k.id === w.app);
    return a ? { id: a.id, label: a.label, image: a.image, cport: a.cport, defaultPort: a.port, extraPorts: a.extraPorts || [], mem: a.memMB, heavy: a.heavy, env: a.env, dirs: a.dirs, icon: a.icon as IconName, custom: false, local: !!localImage(a.image), note: a.note } : null;
  }
  if (w.source === 'compose') {
    const p = w.parsed; if (!p || p.err !== undefined) return null;
    return { label: p.services[0], image: p.images.join(', '), cport: p.ports.length ? +p.ports[0][1] : 80, defaultPort: p.ports.length ? +p.ports[0][0] : 8090,
      extraPorts: p.ports.slice(1).map((q) => q[0] + '→' + q[1] + '/tcp'), mem: null, env: [], dirs: [], icon: 'boxes', custom: true, compose: true, nServices: p.services.length };
  }
  if (!validImage(w.image) || !validCport(w.cport)) return null;
  const loc = localImage(w.image);
  const img = loc ? imgRef(loc) : withTag(w.image.trim());
  const vols = loc ? loc.volumes : ['/data'];
  const seen: Record<string, number> = {};
  const dirs = vols.map((v) => { let n = slug(v.split('/').filter(Boolean).pop() || 'data').replace(/^-/, '') || 'data'; seen[n] = (seen[n] || 0) + 1; if (seen[n] > 1) n += '-' + seen[n]; return { name: n, path: v }; });
  const cp = parseInt(w.cport, 10);
  return { label: img.split('/').pop()!.split(':')[0], image: img, cport: cp, defaultPort: cp >= 1024 ? cp : 8090, extraPorts: [], mem: null, env: [], dirs, icon: 'boxes', custom: true, local: !!loc };
}

function usedPorts(d: Derived) {
  const u: Record<number, string> = { 22: 'el acceso SSH del servidor', 8420: 'este panel' };
  d.ctrs.forEach((c) => c.ports.forEach((p) => { const m = /^(\d+)→/.exec(p); if (m) u[+m[1]] = c.name; }));
  return u;
}
const freePort = (u: Record<number, string>, p: number) => { p = Math.max(1024, p); while (u[p]) p++; return p; };

function validate(w: WizState, d: Derived, used: Record<number, string>) {
  const r: { nameErr?: string; portErr?: string; suggest?: number } = {};
  if (!/^[a-z][a-z0-9-]{1,29}$/.test(w.name || '')) r.nameErr = 'Usa de 2 a 30 caracteres: minúsculas, números y guiones, empezando por una letra.';
  else if (d.ctrs.some((c) => c.name === w.name)) r.nameErr = 'Ya hay un contenedor llamado ' + w.name + '.';
  if (w.source === 'compose') return r;
  const p = parseInt(w.port, 10);
  if (!(String(p) === String(w.port).trim() && p >= 1024 && p <= 65535)) r.portErr = 'Elige un número entre 1024 y 65535.';
  else if (used[p]) { r.portErr = 'El puerto ' + p + ' ya lo usa ' + used[p] + '.'; r.suggest = freePort(used, p + 1); }
  return r;
}

type Check = { sev: 'ok' | 'warn' | 'err' | 'info'; title: string; detail: string; fix?: { label: string; run: () => void } };

function checks(w: WizState, d: Derived, sp: Spec, v: ReturnType<typeof validate>): Check[] {
  const out: Check[] = [];
  const add = (sev: Check['sev'], title: string, detail: string, fix?: Check['fix']) => out.push({ sev, title, detail, fix });
  add(v.nameErr ? 'err' : 'ok', 'Nombre', v.nameErr || w.name + ' está disponible.');
  if (w.source === 'compose') {
    const u = usedPorts(d), clash = (w.parsed && 'services' in w.parsed ? w.parsed.ports : []).find((p) => u[+p[0]]);
    add(clash ? 'err' : 'ok', 'Puertos', clash ? 'El puerto ' + clash[0] + ' ya lo usa ' + u[+clash[0]] + '. Cámbialo en el archivo.' : 'Los puertos del archivo están libres.');
  } else add(v.portErr ? 'err' : 'ok', 'Puerto', v.portErr || 'El puerto ' + w.port + ' está libre.', v.suggest ? { label: 'Usar ' + v.suggest, run: () => setWiz({ port: String(v.suggest) }) } : undefined);

  const m = d.s.metrics;
  const avail = m ? m.mem.totalMB - m.mem.usedMB : null;
  const lim = ({ none: null, '256': 256, '512': 512, '1024': 1024 } as const)[w.memLimit];
  if (avail == null) add('warn', 'Memoria', 'No hay lectura de memoria del servidor, así que no se puede comprobar si cabe.');
  else if (sp.mem == null) {
    if (lim) add('ok', 'Memoria', 'No se puede estimar el consumo de una imagen propia, pero queda limitada a ' + memLabel(lim) + '. Ahora hay ' + memLabel(avail) + ' disponibles.');
    else if (w.source !== 'compose') add('warn', 'Memoria', 'No se puede estimar cuánto consumirá. Conviene fijar un límite para proteger a Nextcloud y al resto de servicios.', { label: 'Limitar a 512 MB', run: () => setWiz({ memLimit: '512' }) });
    else add('info', 'Memoria', 'Ahora hay ' + memLabel(avail) + ' disponibles. Para limitar cada servicio, añade mem_limit en el archivo.');
  } else {
    const left = avail - sp.mem;
    const txt = 'Suele usar unos ' + memLabel(sp.mem) + '; quedarían unos ' + memLabel(Math.max(left, 0)) + ' libres.';
    if (lim && lim < sp.mem) add('warn', 'Memoria', 'El límite de ' + memLabel(lim) + ' es menor que su consumo habitual (unos ' + memLabel(sp.mem) + '): el contenedor podría reiniciarse por falta de memoria.', { label: 'Quitar el límite', run: () => setWiz({ memLimit: 'none' }) });
    else if (left < 512) add('err', 'Memoria', 'No hay memoria suficiente: necesitaría unos ' + memLabel(sp.mem) + ' y solo quedan ' + memLabel(Math.max(avail, 0)) + ' disponibles.');
    else if (left < 2048 || sp.heavy) add('warn', 'Memoria', txt + ' Funcionará, pero el servidor irá más justo.');
    else add('ok', 'Memoria', txt);
  }
  const root = d.s.config.dataRoot;
  const disk = (m?.disks || []).filter((k) => root === k.mount || root.startsWith(k.mount.endsWith('/') ? k.mount : k.mount + '/')).sort((a, b) => b.mount.length - a.mount.length)[0];
  if (sp.dirs.length || w.source === 'compose') {
    if (disk) {
      const pct = Math.round((disk.usedGB / disk.totalGB) * 100), free = Math.round(disk.totalGB - disk.usedGB);
      add(pct >= 90 ? 'err' : pct >= 80 ? 'warn' : 'ok', 'Almacenamiento', 'Los datos irán a ' + root + '/' + (w.name || 'nombre') + ', en ' + disk.label + ' (' + pct + ' % usado, ' + free + ' GB libres).');
    } else add('info', 'Almacenamiento', 'Los datos irán a ' + root + '/' + (w.name || 'nombre') + '.');
  } else add('ok', 'Almacenamiento', 'No guarda datos propios. Solo ocupa el espacio de la imagen.');
  if (w.source !== 'compose') {
    if (w.access === 'tailscale') {
      if (d.tsOk) add('info', 'Acceso', 'Se publicará con HTTPS en https://' + d.s.tailscale.self?.dnsName + ':' + w.port + ', solo para tus dispositivos de Tailscale. Requiere tener activados los certificados HTTPS en la consola de Tailscale.');
      else add('err', 'Acceso', 'Tailscale no está conectado en el servidor, así que no se puede publicar con HTTPS.', { label: 'Usar el puerto directo', run: () => setWiz({ access: 'lan' }) });
    } else if (sp.id === 'vaultwarden') add('warn', 'Acceso', 'Las apps de Bitwarden exigen HTTPS. Con el puerto directo, la app del iPhone no podrá conectarse.', d.tsOk ? { label: 'Usar HTTPS con Tailscale', run: () => setWiz({ access: 'tailscale' }) } : undefined);
    else add('ok', 'Acceso', 'Se abrirá en http://' + d.s.server.ip + ':' + w.port + ', desde la red local y Tailscale.');
  }
  if (sp.local) add('ok', 'Imagen', 'Ya está en el servidor: no hace falta descargarla.');
  else if (sp.custom) add('info', 'Compatibilidad', 'Al descargarla se comprobará que la imagen existe para amd64, la arquitectura de tu servidor.');
  else add('ok', 'Compatibilidad', 'Plantilla preparada para servidores amd64 como el tuyo.');
  if (sp.compose && (sp.nServices || 0) > 1) add('info', 'Pila', 'El archivo define ' + sp.nServices + ' servicios: se desplegarán juntos y se gestionarán como un grupo.');
  return out;
}

function yaml(w: WizState, sp: Spec) {
  if (sp.compose) return '# Se usará tu archivo tal cual.\n' + w.compose.trim();
  const L = ['services:', '  ' + w.name + ':', '    image: ' + sp.image, '    container_name: ' + w.name, '    restart: ' + (w.restart === 'always' ? 'unless-stopped' : 'no'), '    ports:', '      - "' + w.port + ':' + sp.cport + '"'];
  sp.extraPorts.forEach((p) => { const m = /^(\d+)→(\d+)\/(\w+)/.exec(p); if (m) L.push('      - "' + m[1] + ':' + m[2] + '/' + m[3] + '"'); });
  if (sp.dirs.length) { L.push('    volumes:'); sp.dirs.forEach((d) => L.push('      - ' + get().snap?.config.dataRoot + '/' + w.name + '/' + d.name + ':' + d.path)); }
  if (sp.env.length) { L.push('    environment:'); sp.env.forEach((e) => L.push('      ' + e.k + ': ' + (e.secret ? '${' + e.k + '}   # secreto guardado en el servidor' : e.v))); }
  if (w.memLimit !== 'none') L.push('    mem_limit: ' + w.memLimit + 'm');
  if (w.access === 'tailscale') L.push('', '# Después: tailscale serve --bg --https=' + w.port + ' http://127.0.0.1:' + w.port);
  return L.join('\n');
}

/* ---------- Avance ---------- */
function next() {
  const s = get(), w = s.wiz; if (!w) return;
  const sp = spec(w);
  if (w.step === 1) {
    if (!sp) return;
    const key = w.source + ':' + (w.source === 'catalog' ? w.app : w.source === 'image' ? w.image + '/' + w.cport : w.compose);
    if (key === w.initKey) { setWiz({ step: 2 }); return; }
    const names = s.snap?.docker.containers.map((c) => c.name) || [];
    const base = slug(sp.label); let name = base, n = 2;
    while (names.includes(name)) name = base + '-' + n++;
    const used: Record<number, string> = {};
    s.snap?.docker.containers.forEach((c) => c.ports.forEach((p) => { const m = /^(\d+)→/.exec(p); if (m) used[+m[1]] = c.name; }));
    used[22] = 'ssh'; used[8420] = 'umbral';
    const port = used[sp.defaultPort] ? freePort(used, sp.defaultPort + 1) : sp.defaultPort;
    setWiz({ step: 2, initKey: key, name, port: String(port), access: sp.id === 'vaultwarden' && s.snap?.tailscale.running ? 'tailscale' : 'lan', memLimit: 'none', submitErr: null });
  } else if (w.step === 2) setWiz({ step: 3, submitErr: null });
  else if (w.step === 3) void submit();
  else { set({ wiz: null, svcTab: 'containers' }); }
}

async function submit() {
  const w = get().wiz; if (!w) return;
  const sp = spec(w); if (!sp) return;
  try {
    const job = await adapter().deploy({
      source: w.source, app: w.app || undefined, image: w.source === 'image' ? sp.image : undefined, compose: w.source === 'compose' ? w.compose : undefined,
      name: w.name, port: parseInt(w.port, 10) || 0, cport: sp.cport, extraPorts: sp.extraPorts, access: w.access, restart: w.restart,
      memLimitMB: w.memLimit === 'none' ? null : parseInt(w.memLimit, 10), env: [], dirs: sp.dirs,
    });
    setWiz({ step: 4, jobId: job.id, job, submitErr: null });
    await follow(job.id, (j) => setWiz({ job: j }));
  } catch (e) {
    setWiz({ submitErr: e instanceof Error ? e.message : String(e) });
  }
}

/* ---------- Vista ---------- */
export function Wizard({ d }: { d: Derived }) {
  const s = useStore();
  const w = s.wiz!;
  const ref = useRef<HTMLDivElement>(null);
  const sp = spec(w);
  const used = usedPorts(d);
  const v = w.step >= 2 && sp ? validate(w, d, used) : {};
  const ck = w.step === 3 && sp ? checks(w, d, sp, v) : [];
  const nErr = ck.filter((c) => c.sev === 'err').length;
  const job = w.job;
  const done = w.step === 4 && job?.state === 'done';
  const failed = w.step === 4 && job?.state === 'error';
  const running = w.step === 4 && !done && !failed;
  const imgOk = validImage(w.image), cpOk = validCport(w.cport);
  const ready1 = w.source === 'catalog' ? !!w.app && !!sp : w.source === 'compose' ? !!(w.parsed && !w.parsed.err) : imgOk && cpOk;
  const close = () => { if (!running) set({ wiz: null }); };

  useEffect(() => { ref.current?.querySelector<HTMLElement>('.wiz-b input, .wiz-b textarea, .wiz-b button')?.focus(); }, [w.step]);
  useEffect(() => {
    const k = (e: KeyboardEvent) => { if (e.key === 'Escape') close(); };
    document.addEventListener('keydown', k);
    return () => document.removeEventListener('keydown', k);
  });

  const seg = <K extends 'source' | 'access' | 'restart' | 'memLimit'>(key: K, opts: [WizState[K], string][], label: string) =>
    <Seg value={w[key] as string} options={opts as [string, string][]} label={label} onChange={(val) => setWiz({ [key]: val } as Partial<WizState>)} />;

  const sevB = { ok: ['ok', 'Correcto'], warn: ['warn', 'Revisar'], err: ['err', 'Bloquea'], info: ['noCheck', 'Al desplegar'] } as const;
  const footNote = w.submitErr ? '' : w.step === 3 ? (nErr ? (nErr === 1 ? 'Hay 1 problema que impide desplegar.' : 'Hay ' + nErr + ' problemas que impiden desplegar.') : 'Se creará en el servidor al pulsar Desplegar.') : running ? 'Puedes cerrar el panel: el despliegue sigue en el servidor.' : '';
  const nextLabel = w.step === 1 ? 'Continuar' : w.step === 2 ? 'Revisar' : w.step === 3 ? 'Desplegar' : done ? 'Ver en servicios' : failed ? 'Cerrar' : 'Desplegando…';
  const nextDisabled = w.step === 1 ? !ready1 : w.step === 2 ? !!(v.nameErr || v.portErr) : w.step === 3 ? nErr > 0 : running;

  return (
    <>
      <div className="scrim" style={{ zIndex: 50 }} />
      <div className="modal-wrap">
        <div className="wiz glass" role="dialog" aria-modal="true" aria-labelledby="wiz-t" ref={ref}>
          <div className="wiz-h">
            <div><h2 id="wiz-t">Desplegar un servicio</h2><span className="sub">{w.step === 4 ? (done ? 'Terminado' : failed ? 'No se pudo desplegar' : 'Desplegando…') : 'Paso ' + w.step + ' de 4: ' + ['origen', 'configuración', 'revisión'][w.step - 1]}</span></div>
            <button className="round" onClick={close} disabled={running} aria-label="Cerrar el asistente" title="Cerrar"><Icon name="close" /></button>
          </div>
          <ol className="steps" aria-label="Progreso">
            {['Origen', 'Configuración', 'Revisión', 'Despliegue'].map((l, i) => { const n = i + 1; return <li key={l} className={n < w.step || (n === 4 && done) ? 'done' : n === w.step ? 'cur' : ''}><span>{l}</span></li>; })}
          </ol>

          <div className="wiz-b">
            {w.step === 1 && (
              <>
                <div className="field"><span className="f-l">¿Qué quieres desplegar?</span><div>{seg('source', [['catalog', 'Catálogo'], ['compose', 'Docker Compose'], ['image', 'Imagen']], 'Origen')}</div></div>
                {w.source === 'catalog' && (
                  <>
                    <p className="f-msg">Aplicaciones ligeras preparadas para tu servidor, con puertos, carpetas y variables ya resueltos. Las que necesitan varios contenedores, como Immich, se despliegan con Docker Compose.</p>
                    {!s.catalog ? <p className="f-msg"><Icon name="loader" className="i i-sm spin" /> Cargando el catálogo…</p> : (
                      <div className="apps" role="radiogroup" aria-label="Aplicación">
                        {s.catalog.map((a) => (
                          <label key={a.id} className={'opt' + (w.app === a.id ? ' on' : '')}>
                            <input type="radio" name="w-app" checked={w.app === a.id} onChange={() => setWiz({ app: a.id })} />
                            <span className="ico-box"><Icon name={(a.icon as IconName) || 'boxes'} /></span>
                            <span style={{ minWidth: 0 }}><span className="strong" style={{ display: 'block' }}>{a.label}</span><span className="sub">{a.desc}</span>
                              <span className="app-meta"><span className="sub">Suele usar {memLabel(a.memMB)}</span>{a.heavy && <span className="tag">Exigente para tu servidor</span>}</span></span>
                          </label>
                        ))}
                      </div>
                    )}
                  </>
                )}
                {w.source === 'compose' && (
                  <>
                    <div className="field">
                      <label className="f-l" htmlFor="w-cmp">Contenido del archivo docker-compose.yml</label>
                      <textarea id="w-cmp" className="inp mono" rows={9} spellCheck={false} placeholder="Pega aquí tu archivo docker-compose.yml" value={w.compose} onChange={(e) => setWiz({ compose: e.target.value, parsed: null })} />
                    </div>
                    <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
                      <button className="btn btn-sm" onClick={() => setWiz({ parsed: parseCompose(w.compose) })} disabled={!w.compose.trim()}>Analizar archivo</button>
                      <button className="btn btn-sm btn-quiet" onClick={() => setWiz({ compose: 'services:\n  it-tools:\n    image: corentinth/it-tools:latest\n    restart: unless-stopped\n    ports:\n      - "8080:80"\n', parsed: null })}>Usar un ejemplo</button>
                    </div>
                    {w.parsed && 'services' in w.parsed && (
                      <div className="banner ok"><Icon name="check" /><div><b>Archivo válido</b><span>{(w.parsed.services.length === 1 ? 'Un servicio: ' + w.parsed.services[0] : w.parsed.services.length + ' servicios: ' + w.parsed.services.join(', ')) + '. Imagen: ' + w.parsed.images.join(', ') + '. ' + (w.parsed.ports.length ? 'Puertos: ' + w.parsed.ports.map((q) => q[0] + '→' + q[1]).join(', ') + '.' : 'No publica puertos.')}</span></div></div>
                    )}
                    {w.parsed?.err && <div className="banner err"><Icon name="x" /><div><b>No se puede usar este archivo</b><span>{w.parsed.err}</span></div></div>}
                  </>
                )}
                {w.source === 'image' && (
                  <>
                    <div className="fgrid">
                      <div className="field"><label className="f-l" htmlFor="w-img">Imagen</label>
                        <input id="w-img" className={'inp mono' + (w.image && !imgOk ? ' bad' : '')} placeholder="por ejemplo, louislam/uptime-kuma:1" value={w.image} spellCheck={false} autoComplete="off"
                          onChange={(e) => { const val = e.target.value, li = localImage(val); setWiz({ image: val, ...(li?.exposedPort ? { cport: String(li.exposedPort) } : {}) }); }} />
                        <span className={'f-msg' + (w.image && !imgOk ? ' bad' : '')}>{w.image && !imgOk ? 'Formato no válido. Usa nombre:etiqueta, por ejemplo, nginx:1.27.' : imgOk && localImage(w.image) ? 'Ya está en el servidor: no hará falta descargarla.' : 'De Docker Hub o de un registro como ghcr.io. Se descargará al desplegar.'}</span></div>
                      <div className="field"><label className="f-l" htmlFor="w-cp">Puerto que usa la aplicación dentro del contenedor</label>
                        <input id="w-cp" className={'inp' + (cpOk ? '' : ' bad')} inputMode="numeric" value={w.cport} onChange={(e) => setWiz({ cport: e.target.value })} />
                        <span className={'f-msg' + (cpOk ? '' : ' bad')}>{cpOk ? 'Lo indica la documentación de la imagen.' : 'Escribe un número entre 1 y 65535.'}</span></div>
                    </div>
                    {!!s.snap?.docker.images.filter((i) => i.repo !== '<none>').length && (
                      <div className="field"><span className="f-l">O elige una imagen que ya está en el servidor</span>
                        <div className="chips">{s.snap!.docker.images.filter((i) => i.repo !== '<none>').map((i) => { const r = imgRef(i), on = withTag(w.image.trim()) === r; return <button key={r} className={'chip mono' + (on ? ' on' : '')} aria-pressed={on} onClick={() => setWiz({ image: r, cport: String(i.exposedPort || 80) })}>{r}</button>; })}</div></div>
                    )}
                  </>
                )}
              </>
            )}

            {w.step === 2 && sp && (
              <>
                <div className="fgrid">
                  <div className="field"><label className="f-l" htmlFor="w-name">Nombre</label><input id="w-name" className={'inp mono' + (v.nameErr ? ' bad' : '')} value={w.name} onChange={(e) => setWiz({ name: e.target.value })} autoComplete="off" spellCheck={false} /><span className={'f-msg' + (v.nameErr ? ' bad' : '')}>{v.nameErr || 'También será el nombre de su carpeta de datos.'}</span></div>
                  {w.source !== 'compose' && (
                    <div className="field"><label className="f-l" htmlFor="w-port">Puerto en el servidor</label><input id="w-port" className={'inp' + (v.portErr ? ' bad' : '')} inputMode="numeric" value={w.port} onChange={(e) => setWiz({ port: e.target.value })} />
                      <span className={'f-msg' + (v.portErr ? ' bad' : '')}>{v.portErr || 'Libre.'}{v.suggest && <> <button className="link-btn" style={{ fontSize: 12.5 }} onClick={() => setWiz({ port: String(v.suggest) })}>Usar {v.suggest}</button></>}</span></div>
                  )}
                </div>
                {w.source !== 'compose' ? (
                  <>
                    <div className="field"><span className="f-l">Cómo se accede</span><div>{seg('access', [['lan', 'Puerto directo'], ['tailscale', 'HTTPS con Tailscale']], 'Cómo se accede')}</div>
                      <span className="f-msg">{w.access === 'tailscale' ? 'Se abrirá en https://' + (d.s.tailscale.self?.dnsName || 'tu-servidor.ts.net') + ':' + (w.port || 'puerto') + ', solo desde tus dispositivos de Tailscale.' : 'Se abrirá en http://' + d.s.server.ip + ':' + (w.port || 'puerto') + ', desde la red local y Tailscale.'}</span></div>
                    <div className="fgrid">
                      <div className="field"><span className="f-l">Reinicio automático</span><div>{seg('restart', [['always', 'Siempre, salvo si lo detengo'], ['never', 'Nunca']], 'Reinicio automático')}</div></div>
                      <div className="field"><span className="f-l">Límite de memoria</span><div>{seg('memLimit', [['none', 'Sin límite'], ['256', '256 MB'], ['512', '512 MB'], ['1024', '1 GB']], 'Límite de memoria')}</div></div>
                    </div>
                    <div className="field"><span className="f-l">Imagen y datos</span>
                      <div className="kv">
                        <div><span className="muted">Imagen</span><span className="mono">{sp.image}</span></div>
                        <div><span className="muted">Puerto del contenedor</span><span>{sp.cport}</span></div>
                        {sp.extraPorts.length > 0 && <div><span className="muted">Otros puertos</span><span>{sp.extraPorts.join(', ')}</span></div>}
                        <div><span className="muted">Carpetas de datos</span><span className="mono">{sp.dirs.length ? sp.dirs.map((x) => d.s.config.dataRoot + '/' + (w.name || 'nombre') + '/' + x.name).join(', ') : 'Ninguna'}</span></div>
                      </div>
                    </div>
                    {sp.env.length > 0 && (
                      <div className="field"><span className="f-l">Variables de entorno</span>
                        <div className="kv">{sp.env.map((e) => <div key={e.k}><span className="mono">{e.k}</span><span className={e.secret ? 'muted' : ''}>{e.secret ? 'Se generará y se guardará en el servidor' : e.v}</span></div>)}</div>
                        <span className="f-msg">Los secretos nunca se muestran en el panel.</span>
                      </div>
                    )}
                  </>
                ) : (
                  <p className="f-msg">El archivo se guardará en {d.s.config.dataRoot}/{w.name || 'nombre'}/docker-compose.yml y se desplegará con docker compose. Los puertos, el reinicio y los límites son los del archivo.</p>
                )}
              </>
            )}

            {w.step === 3 && sp && (
              <>
                <div className="cklist">
                  {ck.map((c) => { const b = st(sevB[c.sev][0], sevB[c.sev][1]); return (
                    <div className="ck" key={c.title}>
                      <span className={'badge b-' + b.tone}><Sym glyph={b.glyph} /></span>
                      <div style={{ flex: 1, minWidth: 0 }}><span className="strong" style={{ display: 'block' }}>{c.title}</span><span className="sub">{c.detail}</span>
                        {c.fix && <button className="link-btn" style={{ marginTop: 4, fontSize: 13 }} onClick={c.fix.run}>{c.fix.label}</button>}</div>
                      <span className={'badge b-' + b.tone + ' hide-sm'}>{b.label}</span>
                    </div>); })}
                </div>
                {sp.note && <div className="banner setup"><Icon name="info" /><div><span>{sp.note}</span></div></div>}
                {w.submitErr && <div className="banner err"><Icon name="x" /><div><b>El servidor rechazó el despliegue</b><span>{w.submitErr}</span></div></div>}
                <details className="det"><summary>Ver la configuración equivalente en docker-compose</summary><pre className="code mono">{yaml(w, sp)}</pre></details>
              </>
            )}

            {w.step === 4 && (
              <>
                {done && (
                  <div className="done-hero">
                    <div className="hero-ic t-ok"><svg className="i" viewBox="0 0 24 24" aria-hidden="true"><path d={glyphPath('check')} /></svg></div>
                    <h3 style={{ margin: 0, fontSize: 20, fontWeight: 700, letterSpacing: '-.02em' }}>Despliegue completado</h3>
                    <p className="muted" style={{ margin: 0, maxWidth: '52ch' }}>{job?.message}</p>
                    {job?.result?.url && <button className="btn" onClick={() => openUrl(w.name, job.result!.url)}><Icon name="external" />Abrir {sp?.label || w.name}</button>}
                  </div>
                )}
                {failed && <div className="banner err"><Icon name="x" /><div><b>El despliegue se detuvo</b><span>{job?.message}</span></div></div>}
                <div className="cklist" aria-live="polite">
                  {(job?.steps || []).map((p, i) => {
                    const b = p.state === 'done' ? st('resOk', 'Hecho') : p.state === 'running' ? st('starting', 'En curso') : p.state === 'error' ? st('resErr', 'Error') : st('noCheck', 'Pendiente');
                    return (
                      <div className="ck" key={i}>
                        <Badge s={b} iconOnly />
                        <div style={{ flex: 1, minWidth: 0 }}><span className="strong" style={{ display: 'block' }}>{p.label}</span><span className="sub">{b.label}{p.state === 'running' && job?.progress ? ' · ' + job.progress + ' %' : ''}</span></div>
                      </div>
                    );
                  })}
                </div>
              </>
            )}
          </div>

          <div className="wiz-f">
            {(w.step === 2 || w.step === 3) && <button className="btn btn-quiet" onClick={() => setWiz({ step: (w.step - 1) as 1 | 2, submitErr: null })}>Atrás</button>}
            {(done || failed) && <button className="btn btn-quiet" onClick={() => openWizard()}>Desplegar otro</button>}
            <span className="note">{footNote}</span>
            {w.step === 1 && <button className="btn" onClick={close}>Cancelar</button>}
            <button className="btn btn-primary" onClick={() => (failed ? set({ wiz: null }) : next())} disabled={nextDisabled}>{nextLabel}</button>
          </div>
        </div>
      </div>
    </>
  );
}
