import { useCallback, useEffect, useMemo, useRef, useState } from 'react';

/**
 * SearchSelect — a type-to-filter picker for lists too long to scan in a
 * <select>, such as the employee roster.
 *
 * Props:
 *   options    [{ value, label, sub? }]  sub renders as dimmed secondary text
 *   value      currently selected value ('' = nothing)
 *   onChange   (value, option|null) => void
 *   placeholder / emptyText / disabled / allowClear
 *
 * Keyboard: ↑/↓ move, Enter picks, Esc closes, Backspace on an empty query
 * clears the selection. The input doubles as the value display, so there is one
 * control rather than a box plus a separate search field.
 */
export default function SearchSelect({
  options = [],
  value = '',
  onChange,
  placeholder = 'Cari…',
  emptyText = 'Tidak ada hasil',
  disabled = false,
  allowClear = true,
  id,
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [cursor, setCursor] = useState(0);
  const boxRef = useRef(null);
  const inputRef = useRef(null);
  const listRef = useRef(null);

  const selected = useMemo(
    () => options.find((o) => String(o.value) === String(value)) || null,
    [options, value],
  );

  const matches = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return options;
    return options.filter((o) =>
      `${o.label} ${o.sub || ''}`.toLowerCase().includes(q));
  }, [options, query]);

  // Keep the highlighted row in view as the cursor moves past the fold.
  useEffect(() => {
    if (!open || !listRef.current) return;
    listRef.current.querySelector('[data-cursor="true"]')?.scrollIntoView({ block: 'nearest' });
  }, [cursor, open]);

  const close = useCallback(() => { setOpen(false); setQuery(''); }, []);

  useEffect(() => {
    if (!open) return;
    const onDown = (e) => { if (boxRef.current && !boxRef.current.contains(e.target)) close(); };
    document.addEventListener('mousedown', onDown);
    return () => document.removeEventListener('mousedown', onDown);
  }, [open, close]);

  const pick = (opt) => {
    onChange?.(opt ? opt.value : '', opt);
    close();
    inputRef.current?.blur();
  };

  const onKeyDown = (e) => {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      if (!open) { setOpen(true); return; }
      setCursor((c) => {
        const n = matches.length;
        if (!n) return 0;
        return e.key === 'ArrowDown' ? (c + 1) % n : (c - 1 + n) % n;
      });
    } else if (e.key === 'Enter') {
      if (open && matches[cursor]) { e.preventDefault(); pick(matches[cursor]); }
    } else if (e.key === 'Escape') {
      if (open) { e.preventDefault(); close(); }
    } else if (e.key === 'Backspace' && !query && selected && allowClear) {
      pick(null);
    }
  };

  return (
    <div className="ss" ref={boxRef}>
      <div className={`ss-control${open ? ' open' : ''}${disabled ? ' disabled' : ''}`}>
        <input
          id={id}
          ref={inputRef}
          className="ss-input"
          disabled={disabled}
          autoComplete="off"
          role="combobox"
          aria-expanded={open}
          aria-autocomplete="list"
          value={open ? query : (selected?.label || '')}
          placeholder={selected ? selected.label : placeholder}
          onFocus={() => { setOpen(true); setCursor(0); }}
          onChange={(e) => { setQuery(e.target.value); setOpen(true); setCursor(0); }}
          onKeyDown={onKeyDown}
        />
        {allowClear && selected && !disabled && (
          <button type="button" className="ss-clear" aria-label="Hapus pilihan"
            onMouseDown={(e) => { e.preventDefault(); pick(null); }}>×</button>
        )}
        <span className="ss-caret" aria-hidden="true">▾</span>
      </div>

      {open && (
        <div className="ss-menu" ref={listRef} role="listbox">
          {matches.length === 0 && <div className="ss-empty">{emptyText}</div>}
          {matches.map((o, i) => (
            <button
              key={o.value}
              type="button"
              role="option"
              aria-selected={String(o.value) === String(value)}
              data-cursor={i === cursor ? 'true' : undefined}
              className={`ss-option${i === cursor ? ' cursor' : ''}${String(o.value) === String(value) ? ' selected' : ''}`}
              onMouseEnter={() => setCursor(i)}
              onMouseDown={(e) => { e.preventDefault(); pick(o); }}
            >
              <span className="ss-option-label">{o.label}</span>
              {o.sub && <span className="ss-option-sub">{o.sub}</span>}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
