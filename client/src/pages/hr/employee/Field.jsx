

export function Field({ label, value }) {
  return (
    <div style={{ marginBottom: '0.85rem' }}>
      <div style={{ fontSize: '0.75rem', color: '#8a93a6', textTransform: 'uppercase', letterSpacing: '0.03em' }}>{label}</div>
      <div style={{ fontWeight: 500 }}>{value || '-'}</div>
    </div>
  );
}
