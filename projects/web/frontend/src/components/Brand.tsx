export function Brand({ dark = false, compact = false }: { dark?: boolean; compact?: boolean }) {
  return (
    <div className={`brand-lockup min-w-0 ${compact ? 'brand-compact' : ''} ${dark ? 'brand-on-dark' : ''}`} aria-label="УлГТУ Система Лабораторных Работ">
      <strong className="brand-word block">УлГТУ</strong>
      <span className="brand-caption mt-2 block">Система Лабораторных Работ</span>
    </div>
  );
}
