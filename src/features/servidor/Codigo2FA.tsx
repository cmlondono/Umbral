import { useId, useState } from 'react';
import { nav } from '../../state/store';

/* Confirmación con el código del doble factor, para las acciones de copias
 * y del disco externo. El servidor lo vuelve a comprobar. */

export function Falta2FA({ what }: { what: string }) {
  return (
    <p className="f-msg">
      {what} exige la verificación en dos pasos.{' '}
      <button type="button" className="link-btn" style={{ fontSize: 'inherit' }} onClick={() => nav('settings', { setTab: 'security' })}>Actívala en Ajustes › Seguridad y acceso</button>
    </p>
  );
}

const reCode = /^(\d{3} ?\d{3}|[a-z0-9]{4}-[a-z0-9]{4})$/i;

export function Codigo2FA({ submitLabel, danger, disabled, onSubmit, onCancel, autoFocus = true }: {
  submitLabel: string;
  danger?: boolean;
  /** Motivo por el que aún no se puede enviar (se muestra debajo). */
  disabled?: string;
  onSubmit: (code: string) => Promise<void>;
  onCancel: () => void;
  autoFocus?: boolean;
}) {
  const id = useId();
  const [code, setCode] = useState('');
  const [err, setErr] = useState('');
  const [busy, setBusy] = useState(false);
  const ok = reCode.test(code.trim());
  const send = async () => {
    setBusy(true); setErr('');
    try { await onSubmit(code.replace(/\s/g, '')); }
    catch (e) { setErr(e instanceof Error ? e.message : String(e)); setCode(''); }
    setBusy(false);
  };
  return (
    <form onSubmit={(e) => { e.preventDefault(); if (ok && !disabled && !busy) void send(); }} style={{ marginTop: 12 }}>
      <label className="small" htmlFor={id} style={{ display: 'block' }}>Código de tu app de autenticación</label>
      <div className="mem-row" style={{ marginTop: 6 }}>
        <input id={id} className={'inp mono' + (err ? ' bad' : '')} inputMode="numeric" autoComplete="one-time-code" placeholder="123 456" maxLength={9}
          value={code} onChange={(e) => setCode(e.target.value)} autoFocus={autoFocus} spellCheck={false} aria-invalid={!!err} />
        <button className={danger ? 'btn btn-danger-solid' : 'btn btn-primary'} disabled={!ok || !!disabled || busy}>{busy ? 'Comprobando…' : submitLabel}</button>
        <button type="button" className="btn btn-quiet" onClick={onCancel} disabled={busy}>Cancelar</button>
      </div>
      {err ? <p className="f-msg bad" role="alert">{err}</p>
        : <p className="f-msg">{disabled || (ok ? 'Pulsa «' + submitLabel + '» para continuar.' : 'Escribe los 6 dígitos de la app (o un código de recuperación) para continuar.')}</p>}
    </form>
  );
}
