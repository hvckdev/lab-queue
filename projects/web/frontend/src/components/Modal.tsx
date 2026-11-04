import { useEffect, useId, useRef, type ReactNode } from 'react';
import { X } from 'lucide-react';

export function Modal({ open, onClose, title, eyebrow, busy = false, children, wide = false }: { open: boolean; onClose(): void; title: string; eyebrow?: string; busy?: boolean; children: ReactNode; wide?: boolean }) {
  const dialog = useRef<HTMLDialogElement>(null);
  const titleID = useId();

  useEffect(() => {
    const element = dialog.current;
    if (!element) return;
    if (open && !element.open) element.showModal();
    if (!open && element.open) element.close();
  }, [open]);

  return (
    <dialog ref={dialog} className="modal potok-modal" aria-labelledby={titleID} onCancel={(event) => { event.preventDefault(); if (!busy) onClose(); }} onClick={(event) => { if (event.target === event.currentTarget && !busy) onClose(); }}>
      <div className={`modal-box ${wide ? 'max-w-2xl' : 'max-w-lg'} p-0`}>
        <div className="flex items-start justify-between gap-5 border-b border-base-300 px-6 py-6 sm:px-8">
          <div className="min-w-0">{eyebrow && <p className="eyebrow mb-2">{eyebrow}</p>}<h2 id={titleID} className="text-2xl font-extrabold tracking-tight">{title}</h2></div>
          <button type="button" className="btn btn-ghost btn-square btn-sm -mr-2 shrink-0" onClick={onClose} disabled={busy} aria-label="Закрыть окно"><X size={20} /></button>
        </div>
        <div className="px-6 py-6 sm:px-8">{children}</div>
      </div>
    </dialog>
  );
}
