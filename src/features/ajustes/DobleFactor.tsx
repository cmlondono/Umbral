import { useMemo, useState } from 'react';
import qrcode from 'qrcode-generator';
import { adapter, set, toast, useStore } from '../../state/store';
import { Badge, Icon, Panel, st } from '../../design/ui';

/** QR dibujado como SVG, sin imágenes externas. */
function QR({ text }: { text: string }) {
  const cells = useMemo(() => {
    const q = qrcode(0, 'M');
    q.addData(text);
    q.make();
    const n = q.getModuleCount();
    let d = '';
    for (let r = 0; r < n; r++) for (let c = 0; c < n; c++) if (q.isDark(r, c)) d += 'M' + c + ' ' + r + 'h1v1h-1z';
    return { n, d };
  }, [text]);
  return (
    <svg viewBox={'-2 -2 ' + (cells.n + 4) + ' ' + (cells.n + 4)} width="188" height="188" role="img" aria-label="Código QR para la app de autenticación" style={{ background: '#fff', borderRadius: 12, display: 'block' }} shapeRendering="crispEdges">
      <path d={cells.d} fill="#000" />
    </svg>
  );
}

type Phase = { k: 'idle' } | { k: 'pass' } | { k: 'scan'; secret: string; uri: string } | { k: 'codes'; codes: string[] } | { k: 'off' };

export function DobleFactor() {
  const s = useStore();
  const [ph, setPh] = useState<Phase>({ k: 'idle' });
  const [pass, setPass] = useState('');
  const [code, setCode] = useState('');
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  const on = s.totp;
  const reset = () => { setPh({ k: 'idle' }); setPass(''); setCode(''); setErr(''); };
  const go = async (fn: () => Promise<void>) => { setBusy(true); setErr(''); try { await fn(); } catch (e) { setErr(e instanceof Error ? e.message : String(e)); } setBusy(false); };

  const begin = () => go(async () => { const r = await adapter().totpBegin(pass); setPass(''); setPh({ k: 'scan', secret: r.secret, uri: r.uri }); });
  const confirm = () => go(async () => { const r = await adapter().totpConfirm(code.replace(/\s/g, '')); setCode(''); set({ totp: true, recoveryLeft: r.recovery.length }); setPh({ k: 'codes', codes: r.recovery }); });
  const disable = () => go(async () => { await adapter().totpDisable(pass, code.trim()); set({ totp: false, recoveryLeft: 0 }); reset(); toast({ tone: 'ok', title: 'Doble factor desactivado', body: 'Ahora solo se pide la contraseña.' }); });
  const download = (codes: string[]) => {
    const a = document.createElement('a');
    a.href = URL.createObjectURL(new Blob(['Códigos de recuperación de Umbral (' + s.user + ')\nCada uno sirve una sola vez.\n\n' + codes.join('\n') + '\n'], { type: 'text/plain' }));
    a.download = 'umbral-codigos-recuperacion.txt';
    a.click();
    setTimeout(() => URL.revokeObjectURL(a.href), 2000);
  };
  const secretGroups = (x: string) => x.match(/.{1,4}/g)?.join(' ') || x;
  const isPhone = typeof navigator !== 'undefined' && /iphone|ipad|android/i.test(navigator.userAgent);

  return (
    <Panel title="Verificación en dos pasos" hint={<span className="hint" style={{ display: 'block' }}>Un código de tu móvil además de la contraseña</span>} right={<Badge s={on ? st('ok', 'Activada') : st('disabled', 'Desactivada')} />}>
      {ph.k === 'idle' && (
        <div className="setrow">
          <div className="grow">
            <span className="strong">{on ? 'Tu cuenta pide un código al entrar' : 'Protege la cuenta con un código de 6 dígitos'}</span>
            <span className="sub">{on
              ? 'Te quedan ' + s.recoveryLeft + ' códigos de recuperación. Si pierdes el móvil, entra con uno de ellos.'
              : 'Aunque alguien conozca tu contraseña, no podrá entrar sin tu móvil. Sirve la app Contraseñas del iPhone, Google Authenticator, 1Password o similar.'}</span>
          </div>
          {on
            ? <button className="btn btn-sm btn-danger" onClick={() => setPh({ k: 'off' })}>Desactivar</button>
            : <button className="btn btn-sm btn-primary" onClick={() => setPh({ k: 'pass' })}><Icon name="lock" />Activar</button>}
        </div>
      )}

      {(ph.k === 'pass' || ph.k === 'off') && (
        <form className="pb" style={{ display: 'flex', flexDirection: 'column', gap: 14 }} onSubmit={(e) => { e.preventDefault(); void (ph.k === 'pass' ? begin() : disable()); }}>
          {err && <div className="banner err" role="alert"><Icon name="x" /><div><span>{err}</span></div></div>}
          <div className="fgrid">
            <div className="field"><label className="f-l" htmlFor="tf-p">Contraseña del panel</label>
              <input id="tf-p" className="inp" type="password" autoComplete="current-password" value={pass} onChange={(e) => setPass(e.target.value)} autoFocus /></div>
            {ph.k === 'off' && <div className="field"><label className="f-l" htmlFor="tf-c">Código de la app o de recuperación</label>
              <input id="tf-c" className="inp mono" inputMode="text" autoComplete="one-time-code" value={code} onChange={(e) => setCode(e.target.value)} /></div>}
          </div>
          <span className="f-msg">{ph.k === 'pass' ? 'Por seguridad, confirma tu contraseña antes de empezar.' : 'Al desactivarla, el panel volverá a pedir solo la contraseña.'}</span>
          <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
            <button type="button" className="btn btn-sm btn-quiet" onClick={reset}>Cancelar</button>
            <button type="submit" className={'btn btn-sm ' + (ph.k === 'off' ? 'btn-danger-solid' : 'btn-primary')} disabled={busy || !pass || (ph.k === 'off' && !code.trim())}>{busy ? 'Comprobando…' : ph.k === 'off' ? 'Desactivar' : 'Continuar'}</button>
          </div>
        </form>
      )}

      {ph.k === 'scan' && (
        <form className="pb tf-scan" onSubmit={(e) => { e.preventDefault(); void confirm(); }}>
          <QR text={ph.uri} />
          <div style={{ display: 'flex', flexDirection: 'column', gap: 12, minWidth: 0, flex: 1 }}>
            <ol className="small" style={{ margin: 0, paddingLeft: 18, color: 'var(--ink2)', display: 'flex', flexDirection: 'column', gap: 6 }}>
              <li>En el iPhone, abre la cámara y apunta al código, o abre <b>Contraseñas</b> › <b>Códigos</b> › <b>+</b> y escanéalo.</li>
              <li>Si no puedes escanearlo, escribe esta clave a mano:<br /><code className="mono" style={{ userSelect: 'all', fontSize: 13 }}>{secretGroups(ph.secret)}</code></li>
              <li>Escribe aquí el código de 6 dígitos que aparece.</li>
            </ol>
            {isPhone && <a className="btn btn-sm" href={ph.uri} style={{ alignSelf: 'flex-start' }}>Añadir en este móvil</a>}
            {err && <div className="banner err" role="alert"><Icon name="x" /><div><span>{err}</span></div></div>}
            <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
              <input className="inp mono" aria-label="Código de 6 dígitos" inputMode="numeric" autoComplete="one-time-code" maxLength={7} placeholder="123456" value={code} onChange={(e) => setCode(e.target.value)} style={{ width: 130, fontSize: 18, letterSpacing: '.12em' }} autoFocus />
              <button type="submit" className="btn btn-primary" disabled={busy || code.replace(/\s/g, '').length !== 6}>{busy ? 'Comprobando…' : 'Activar'}</button>
              <button type="button" className="btn btn-quiet" onClick={reset}>Cancelar</button>
            </div>
          </div>
        </form>
      )}

      {ph.k === 'codes' && (
        <div className="pb" style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          <div className="banner ok"><Icon name="check" /><div><b>Verificación en dos pasos activada</b><span>Guarda estos códigos de recuperación en un lugar seguro, fuera del móvil. Cada uno sirve una sola vez y no se vuelven a mostrar. Las demás sesiones se han cerrado.</span></div></div>
          <div className="tf-codes mono">{ph.codes.map((c) => <span key={c}>{c}</span>)}</div>
          <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end', flexWrap: 'wrap' }}>
            <button className="btn btn-sm" onClick={() => download(ph.codes)}><Icon name="download" />Descargar como .txt</button>
            <button className="btn btn-sm btn-primary" onClick={reset}>Ya los he guardado</button>
          </div>
        </div>
      )}
    </Panel>
  );
}
