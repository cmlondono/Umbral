import type { Config } from '../../data/types';
import type { Derived } from '../../state/derive';
import { adapter, run, set, useStore } from '../../state/store';
import { ActButton, Badge, Icon, Panel, act, agoIso, st } from '../../design/ui';
import { TOOLS, copySsh, openUrl, pcActions, sshCommand, toolObj } from '../../state/actions';
import { Publicar } from './Publicar';

export function AccesoRemoto({ d }: { d: Derived }) {
  const s = useStore();
  const tool = toolObj(d);
  const choosing = !tool || s.editingTool;
  const draft = s.toolDraft ?? d.s.config.remoteTool;
  const pa = pcActions(d);
  const ts = d.s.tailscale;
  const save = async () => {
    const t = TOOLS.find((k) => k.id === draft);
    if (!t) return;
    const ok = await run('No se pudo guardar', () => adapter().saveConfig({ ...d.s.config, remoteTool: t.id as Config['remoteTool'] }), { title: 'Herramienta guardada: ' + t.label, body: 'El botón para conectarte al PC ya la usará. Se guarda en el servidor.' });
    if (ok) set({ editingTool: false, toolDraft: null });
  };
  const hint = !tool ? 'Elige arriba la herramienta de escritorio remoto para poder conectarte.'
    : d.pcState === 'notConfigured' ? 'Añade la IP y la MAC del PC en Ajustes › Dispositivos registrados.'
    : d.pcState === 'off' ? 'El PC está apagado: enciéndelo con Wake-on-LAN y conéctate cuando aparezca como encendido.'
    : d.pcState === 'unknown' ? 'No se puede saber si el PC está encendido mientras el servidor no responda.'
    : d.pcState === 'waking' ? 'Esperando a que el PC responda.' : 'Listo para conectarte.';

  return (
    <>
      <Panel title="Herramienta de escritorio remoto" id="rt-h" right={tool && !choosing ? <button className="link-btn" onClick={() => set({ editingTool: true, toolDraft: d.s.config.remoteTool })}>Cambiar herramienta</button> : undefined}>
        {choosing ? (
          <div className="pb">
            <p style={{ margin: '0 0 14px', color: 'var(--ink2)' }}>Elige la herramienta que usas para conectarte al PC. El panel solo la abrirá; la conexión la gestiona la propia herramienta.</p>
            <div className="opts" role="radiogroup" aria-label="Herramienta de escritorio remoto">
              {TOOLS.map((t) => (
                <label key={t.id} className={'opt' + (draft === t.id ? ' on' : '')}>
                  <input type="radio" name="remote-tool" checked={draft === t.id} onChange={() => set({ toolDraft: t.id })} />
                  <span><span className="strong" style={{ display: 'block' }}>{t.label}</span><span className="sub">{t.note}</span></span>
                </label>
              ))}
            </div>
            <div style={{ display: 'flex', gap: 8, marginTop: 16, flexWrap: 'wrap' }}>
              <button className="btn btn-primary" onClick={() => void save()} disabled={!draft || !d.srvOn}>Guardar elección</button>
              {tool && <button className="btn btn-quiet" onClick={() => set({ editingTool: false, toolDraft: null })}>Cancelar</button>}
            </div>
          </div>
        ) : (
          <div className="lrow">
            <span className="ico-box"><Icon name="monitor" /></span>
            <div className="grow"><span className="strong">{tool!.label}</span><span className="sub">{tool!.note}</span></div>
            <Badge s={st('ok', 'Elegida')} />
          </div>
        )}
      </Panel>

      <Panel title="Equipos" id="rd-h">
        <div>
          <div className="lrow" style={{ flexWrap: 'wrap' }}>
            <span className="ico-box"><Icon name="monitor" /></span>
            <div className="grow" style={{ minWidth: 180 }}><span className="strong">{d.s.pc.name || 'PC de escritorio'}</span><span className="sub">{(d.s.pc.ip || 'Sin IP') + ' · ' + (tool ? tool.label : 'herramienta sin elegir')}</span></div>
            <Badge s={d.pcState === 'notConfigured' ? st('notConfigured') : st(d.pcState)} />
            <div className="row-actions" style={{ flexWrap: 'wrap' }}>
              {d.pcState !== 'on' && <ActButton a={pa.wake} />}
              <ActButton a={pa.remote} />
            </div>
            <div className="small muted" style={{ flex: '1 1 100%', paddingLeft: 48 }}>{hint}</div>
          </div>
          <div className="lrow" style={{ flexWrap: 'wrap' }}>
            <span className="ico-box"><Icon name="server" /></span>
            <div className="grow" style={{ minWidth: 180 }}><span className="strong">Servidor de casa</span><span className="sub mono">{sshCommand(d)}</span></div>
            <Badge s={st(d.srvOn ? 'online' : 'offline')} />
            <div className="row-actions"><ActButton a={act({ label: 'Copiar comando SSH', icon: 'copy', run: () => void copySsh(d) })} /></div>
          </div>
        </div>
      </Panel>

      <Panel title="Acceso desde fuera de casa" id="vpn-h">
        <div><div className="lrow" style={{ flexWrap: 'wrap' }}>
          <span className="ico-box"><Icon name="lock" /></span>
          <div className="grow" style={{ minWidth: 180 }}><span className="strong">Tailscale</span><span className="sub">{!d.srvOn ? 'Estado desconocido' : !ts.installed ? 'No está instalado en el servidor' : ts.self ? 'El servidor es ' + ts.self.dnsName + ' (' + ts.self.ip + ')' : ts.error || 'Sin conectar'}</span></div>
          <Badge s={!d.srvOn ? st('unknown') : ts.running ? st('connected') : ts.installed ? st('err', 'Desconectado') : st('notConfigured', 'No instalado')} />
          <div className="row-actions"><ActButton a={act({ label: 'Abrir consola de Tailscale', kind: 'external', run: () => openUrl('la consola de Tailscale', 'https://login.tailscale.com/admin/machines') })} /></div>
        </div></div>
        <p className="small muted" style={{ margin: 0, padding: '0 var(--pad) 12px 68px' }}>
          Desde fuera de casa, activa Tailscale en tu iPhone o portátil y abre {ts.self ? (ts.serve?.some((e) => e.local === d.s.panel.port) ? ts.serve.find((e) => e.local === d.s.panel.port)!.url : 'http://' + ts.self.dnsName + ':' + (d.s.panel.port || 8420)) : 'este panel con el nombre de Tailscale del servidor'}. El panel no se expone a Internet: solo responde en la red local y en Tailscale.
        </p>
        {ts.peers.length > 0 && (
          <div style={{ borderTop: '1px solid var(--line)' }}>
            <div className="ph"><h2 style={{ fontSize: 15 }}>Dispositivos en tu red Tailscale</h2></div>
            <div>{ts.peers.map((p) => (
              <div className="lrow" key={p.name + p.ip}>
                <span className="ico-box"><Icon name={/ios|android/i.test(p.os) ? 'phone' : /windows|macos|linux/i.test(p.os) ? 'laptop' : 'devices'} /></span>
                <div className="grow"><span className="strong">{p.name}</span><span className="sub">{p.os + ' · ' + p.ip}</span></div>
                <span className="muted small hide-sm">{p.online ? 'Ahora' : p.lastSeen ? agoIso(p.lastSeen) : ''}</span>
                <Badge s={!d.srvOn ? st('unknown') : p.online ? st('connected') : st('noReply', 'Desconectado')} />
              </div>
            ))}</div>
          </div>
        )}
      </Panel>
      <Publicar d={d} />
    </>
  );
}
