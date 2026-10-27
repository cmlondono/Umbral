import { useEffect, useRef, useState } from 'react';
import { login, useStore } from '../state/store';
import { Icon } from '../design/ui';

export function Login() {
  const s = useStore();
  const [user, setUser] = useState('');
  const [pass, setPass] = useState('');
  const [show, setShow] = useState(false);
  const [remember, setRemember] = useState(true);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState('');
  const [touched, setTouched] = useState(false);
  const [step2, setStep2] = useState(false);
  const [code, setCode] = useState('');
  const codeRef = useRef<HTMLInputElement>(null);
  useEffect(() => { if (step2) codeRef.current?.focus(); }, [step2]);
  const userRef = useRef<HTMLInputElement>(null);
  useEffect(() => { userRef.current?.focus(); }, []);

  const userErr = touched && !user.trim() ? 'Escribe tu usuario.' : '';
  const passErr = touched && !pass ? 'Escribe tu contraseña.' : '';

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setTouched(true);
    if (!user.trim() || !pass) return;
    if (step2 && !code.trim()) return;
    setBusy(true); setErr('');
    const r = await login(user, pass, remember, step2 ? code : undefined);
    setBusy(false);
    if (r === 'needCode') { setStep2(true); return; }
    if (r) {
      setErr(r);
      if (step2) setCode(''); else setPass('');
      if (/contraseña incorrectos|Demasiados/.test(r)) { setStep2(false); setCode(''); setPass(''); }
    }
  };

  return (
    <main className="login">
      <form className="login-card glass" onSubmit={submit} noValidate aria-labelledby="login-t">
        <div className="login-brand">
          <svg width="44" height="44" viewBox="0 0 32 32" aria-hidden="true"><rect x="1" y="1" width="30" height="30" rx="7.5" fill="#1d1d1f" /><path d="M10 23V13.5a6 6 0 0 1 12 0V23" fill="none" stroke="#ffffff" strokeWidth="2.6" strokeLinecap="round" /><circle cx="16" cy="21.5" r="2.3" fill="#30d158" /></svg>
          <h1 id="login-t">Umbral</h1>
          <p>Inicia sesión para administrar tu servidor y tu red.</p>
        </div>

        {!s.authConfigured && (
          <div className="banner setup"><Icon name="wrench" /><div><b>Todavía no hay ningún usuario</b><span>Créalo en el servidor con <code className="mono">sudo -u umbral umbral usuario crear tu-nombre -datos /var/lib/umbral</code> y vuelve aquí.</span></div></div>
        )}
        {!s.online && (
          <div className="banner err"><Icon name="x" /><div><b>El servidor no responde</b><span>Comprueba que el servicio umbral esté en marcha y que estés en la red de casa o en Tailscale.</span></div></div>
        )}
        {err && <div className="banner err" role="alert"><Icon name="x" /><div><span>{err}</span></div></div>}

        {step2 ? (
          <div className="field">
            <label className="f-l" htmlFor="lg-c">Código de verificación</label>
            <input id="lg-c" ref={codeRef} className="inp mono" inputMode="numeric" autoComplete="one-time-code" value={code} onChange={(e) => setCode(e.target.value)} placeholder="123456" style={{ fontSize: 20, letterSpacing: '.15em', textAlign: 'center', height: 48 }} />
            <span className="f-msg">Ábrelo en la app de autenticación de tu móvil. Si no lo tienes, escribe un código de recuperación.</span>
            <button type="button" className="link-btn" style={{ fontSize: 13, alignSelf: 'flex-start' }} onClick={() => { setStep2(false); setCode(''); setPass(''); setErr(''); }}>Volver</button>
          </div>
        ) : (<>
        <div className="field">
          <label className="f-l" htmlFor="lg-u">Usuario</label>
          <input id="lg-u" ref={userRef} className={'inp' + (userErr ? ' bad' : '')} value={user} onChange={(e) => setUser(e.target.value)} autoComplete="username" autoCapitalize="none" spellCheck={false} aria-invalid={!!userErr} aria-describedby={userErr ? 'lg-u-m' : undefined} />
          {userErr && <span id="lg-u-m" className="f-msg bad">{userErr}</span>}
        </div>
        <div className="field">
          <label className="f-l" htmlFor="lg-p">Contraseña</label>
          <div className="pw">
            <input id="lg-p" className={'inp' + (passErr ? ' bad' : '')} type={show ? 'text' : 'password'} value={pass} onChange={(e) => setPass(e.target.value)} autoComplete="current-password" aria-invalid={!!passErr} aria-describedby={passErr ? 'lg-p-m' : undefined} />
            <button type="button" className="pw-eye" onClick={() => setShow(!show)} aria-label={show ? 'Ocultar la contraseña' : 'Mostrar la contraseña'} aria-pressed={show}><Icon name={show ? 'eyeOff' : 'eye'} /></button>
          </div>
          {passErr && <span id="lg-p-m" className="f-msg bad">{passErr}</span>}
        </div>
        <label className="login-remember">
          <input type="checkbox" checked={remember} onChange={(e) => setRemember(e.target.checked)} />
          <span><span className="strong">Mantener la sesión en este dispositivo</span><span className="sub">Durante 30 días. Desmárcalo en equipos que no sean tuyos.</span></span>
        </label>
        </>)}
        <button className="btn btn-primary login-go" type="submit" disabled={busy || !s.authConfigured}>
          {busy && <Icon name="loader" className="i spin" />}{busy ? 'Comprobando…' : step2 ? 'Verificar' : 'Iniciar sesión'}
        </button>
        <p className="login-foot"><Icon name="lock" className="i i-sm" />Solo accesible desde tu red local y Tailscale.</p>
      </form>
    </main>
  );
}
