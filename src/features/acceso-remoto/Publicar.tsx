import { useState } from 'react';
import type { Derived } from '../../state/derive';
import { adapter, confirmModal, run } from '../../state/store';
import { Badge, Icon, Panel, st } from '../../design/ui';
import { openUrl } from '../../state/actions';

interface Candidate { key: string; label: string; sub: string; local: number; https: number }

/* Publicar servicios con HTTPS dentro de la red Tailscale («tailscale serve»).
 * Solo lo ven tus equipos de Tailscale; nunca se abre a Internet. */
export function Publicar({ d }: { d: Derived }) {
  const ts = d.s.tailscale;
  const [busy, setBusy] = useState('');
  if (!ts.installed) return null;
  const serve = ts.serve || [];
  const dns = ts.self?.dnsName || '';
  const panelPort = d.s.panel.port || 8420;

  // Umbral y cada puerto TCP publicado por un contenedor.
  const cands: Candidate[] = [{ key: 'umbral', label: 'Umbral (este panel)', sub: 'Recomendado: HTTPS en casa y fuera, sin puerto en la dirección', local: panelPort, https: 443 }];
  for (const c of d.s.docker.containers) {
    for (const p of c.ports) {
      const m = /^(\d+)→(\d+)\/tcp$/.exec(p);
      if (!m) continue;
      const local = Number(m[1]);
      cands.push({ key: c.name + ':' + local, label: c.name, sub: 'Puerto ' + local + (c.state === 'running' ? '' : ' · detenido'), local, https: local === 443 ? 8443 : local });
    }
  }
  const pub = (c: Candidate) => serve.find((e) => e.local === c.local);
  const others = serve.filter((e) => !cands.some((c) => c.local === e.local));

  const publish = async (c: Candidate) => {
    setBusy(c.key);
    await run('No se pudo publicar', () => adapter().publish(c.https, c.local, c.label), { title: 'Publicado con HTTPS', body: c.label + ' ya responde en https://' + dns + (c.https === 443 ? '' : ':' + c.https) + ' desde tus equipos de Tailscale.' });
    setBusy('');
  };
  const unpublish = (port: number, label: string) => confirmModal({
    title: '¿Dejar de publicar ' + label + '?', body: 'Su dirección https://' + dns + (port === 443 ? '' : ':' + port) + ' dejará de funcionar. El servicio sigue en marcha y accesible como antes.',
    confirmLabel: 'Dejar de publicar', confirmCls: 'btn btn-danger-solid',
    onConfirm: async () => { setBusy('off' + port); await run('No se pudo quitar', () => adapter().unpublish(port, label), { title: 'Ya no se publica', body: label + '.' }); setBusy(''); },
  });
  const blocked = d.serverBlocked || (!ts.running ? 'Tailscale no está conectado en el servidor.' : '');

  return (
    <Panel title="Publicar con HTTPS en Tailscale" id="https-h" hint="Certificado válido y solo para tus equipos de Tailscale">
      <p className="small muted" style={{ margin: 0, padding: 'var(--pad) var(--pad) 6px' }}>
        Tailscale pone un certificado HTTPS de verdad y solo dejan entrar a tus equipos de Tailscale: nunca se abre a Internet. Algunas apps, como Bitwarden, solo funcionan con HTTPS. La primera vez, Tailscale te pedirá activar los certificados HTTPS en su consola.
      </p>
      <div>
        {cands.map((c) => {
          const e = pub(c);
          return (
            <div className="lrow" key={c.key} style={{ flexWrap: 'wrap' }}>
              <span className="ico-box"><Icon name={c.key === 'umbral' ? 'shield' : 'boxes'} /></span>
              <div className="grow" style={{ minWidth: 200 }}>
                <span className="strong">{c.label}</span>
                <span className="sub">{e ? <span className="mono">{e.url}</span> : c.sub}</span>
              </div>
              <Badge s={e ? st('ok', 'Publicado') : st('noCheck', 'Sin publicar')} />
              <div className="row-actions">
                {e ? <>
                  <button className="btn btn-sm" onClick={() => openUrl(c.label, e.url)}><Icon name="external" />Abrir</button>
                  <button className="btn btn-sm btn-quiet btn-danger" onClick={() => unpublish(e.port, c.label)} disabled={!!blocked || !!busy}>Dejar de publicar</button>
                </> : <button className="btn btn-sm" onClick={() => void publish(c)} disabled={!!blocked || !!busy} title={blocked || 'Publicar en https://' + dns + (c.https === 443 ? '' : ':' + c.https)}>{busy === c.key ? 'Publicando…' : 'Publicar'}</button>}
              </div>
            </div>
          );
        })}
        {others.map((e) => (
          <div className="lrow" key={'o' + e.port}>
            <span className="ico-box"><Icon name="external" /></span>
            <div className="grow"><span className="strong">Otra publicación</span><span className="sub mono">{e.url} → {e.target}</span></div>
            <Badge s={st('ok', 'Publicado')} />
            <div className="row-actions"><button className="btn btn-sm btn-quiet btn-danger" onClick={() => unpublish(e.port, e.target)} disabled={!!blocked || !!busy}>Dejar de publicar</button></div>
          </div>
        ))}
      </div>
    </Panel>
  );
}
