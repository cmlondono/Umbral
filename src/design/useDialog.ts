import { useEffect, useRef } from 'react';

/* Bloquea el foco dentro de un diálogo y lo devuelve al cerrarlo. */
export function useDialog(open: boolean, onClose?: () => void) {
  const ref = useRef<HTMLDivElement | null>(null);
  useEffect(() => {
    if (!open) return;
    const prev = document.activeElement as HTMLElement | null;
    const el = ref.current;
    const first = el?.querySelector<HTMLElement>('input, textarea, button.btn-primary, button:not([disabled])');
    first?.focus();
    const key = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && onClose) { e.stopPropagation(); onClose(); }
      if (e.key !== 'Tab' || !el) return;
      const f = Array.from(el.querySelectorAll<HTMLElement>('button:not([disabled]), input, textarea, select, summary, [href]'));
      if (!f.length) return;
      if (e.shiftKey && document.activeElement === f[0]) { e.preventDefault(); f[f.length - 1].focus(); }
      else if (!e.shiftKey && document.activeElement === f[f.length - 1]) { e.preventDefault(); f[0].focus(); }
    };
    document.addEventListener('keydown', key);
    return () => { document.removeEventListener('keydown', key); prev?.focus?.(); };
  }, [open]); // eslint-disable-line react-hooks/exhaustive-deps
  return ref;
}
