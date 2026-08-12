// Small pieces the three template panels share, so a change to the shell's
// rhythm (toolbar, empty state, form card) lands on all three at once. They were
// three separate pages with three slightly different layouts before they were
// merged; keeping the chrome here is what stops that drift returning.

export function Muted({ children }) {
  return <span style={{ color: 'var(--ink-3)', fontWeight: 400 }}>{children}</span>;
}

export function PanelToolbar({ hint, open, onNew, newLabel = 'Template Baru' }) {
  return (
    <div className="tpl-toolbar">
      <p className="tpl-hint">{hint}</p>
      <button type="button" className="btn btn-primary" onClick={onNew}>
        {open ? 'Tutup' : `+ ${newLabel}`}
      </button>
    </div>
  );
}

export function FormCard({ title, children }) {
  return (
    <div className="card tpl-form-card">
      <div className="card-header"><h2>{title}</h2></div>
      {children}
    </div>
  );
}

export function EmptyRow({ cols, children = 'Belum ada template' }) {
  return (
    <tr>
      <td colSpan={cols} style={{ textAlign: 'center', color: 'var(--ink-3)', padding: '2rem' }}>
        {children}
      </td>
    </tr>
  );
}
