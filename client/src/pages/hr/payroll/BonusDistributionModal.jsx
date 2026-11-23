import { useEffect, useState } from 'react';
import { getWageComponents, getPayrollBonusEligible, applyPayrollBonus } from '../../../api';
import { fmtIDR } from '../../../format';
import { ScoreBadge } from './Badges';

export function BonusDistributionModal({ periodId, onClose, onApplied }) {
  const [bonusComponents, setBonusComponents] = useState([]);
  const [selectedWC, setSelectedWC] = useState('');
  const [pot, setPot] = useState('');
  const [eligible, setEligible] = useState([]);
  const [checked, setChecked] = useState({});
  const [loadingEligible, setLoadingEligible] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    getWageComponents().then(({ data }) => {
      const bonus = (Array.isArray(data) ? data : []).filter((c) => c.type === 'bonus');
      setBonusComponents(bonus);
      if (bonus.length === 1) setSelectedWC(bonus[0].id);
    }).catch(() => {});
  }, []);

  useEffect(() => {
    if (!selectedWC) { setEligible([]); setChecked({}); return; }
    setLoadingEligible(true); setError('');
    getPayrollBonusEligible(periodId, selectedWC)
      .then(({ data }) => {
        const rows = Array.isArray(data) ? data : [];
        setEligible(rows);
        const init = {};
        rows.forEach((r) => { init[r.line_component_id] = true; });
        setChecked(init);
      })
      .catch(() => setError('Gagal memuat daftar karyawan'))
      .finally(() => setLoadingEligible(false));
  }, [selectedWC, periodId]);

  const checkedIds = Object.keys(checked).filter((k) => checked[k]);
  const potNum = Number(pot) || 0;
  const perEmployee = checkedIds.length > 0 ? Math.floor(potNum / checkedIds.length) : 0;

  const toggle = (id) => setChecked((c) => ({ ...c, [id]: !c[id] }));
  const toggleAll = () => {
    const allOn = eligible.every((r) => checked[r.line_component_id]);
    const next = {};
    eligible.forEach((r) => { next[r.line_component_id] = !allOn; });
    setChecked(next);
  };

  const apply = async () => {
    if (!selectedWC || checkedIds.length === 0 || perEmployee <= 0) return;
    setBusy(true); setError('');
    try {
      await applyPayrollBonus(periodId, {
        wage_component_id: selectedWC,
        amount_per_employee: perEmployee,
        line_component_ids: checkedIds,
      });
      onApplied();
    } catch (err) {
      setError(err?.response?.data?.error || 'Gagal mendistribusikan bonus');
      setBusy(false);
    }
  };

  return (
    <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,.4)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 200 }} onClick={onClose}>
      <div onClick={(e) => e.stopPropagation()} style={{ background: '#fff', borderRadius: 12, width: 560, maxWidth: '95vw', maxHeight: '90vh', display: 'flex', flexDirection: 'column', boxShadow: '0 8px 32px rgba(0,0,0,.18)' }}>
        <div style={{ padding: '16px 20px', borderBottom: '1px solid #e6e8ee', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          <h2 style={{ margin: 0, fontSize: 18 }}>Distribusi Bonus</h2>
          <button onClick={onClose} style={{ background: 'none', border: 0, fontSize: 22, cursor: 'pointer', color: '#889' }}>×</button>
        </div>

        <div style={{ flex: 1, overflowY: 'auto', padding: 20 }}>
          {error && <div style={{ background: '#fce8e6', color: '#c5221f', padding: 10, borderRadius: 6, marginBottom: 12 }}>{error}</div>}

          <div style={{ marginBottom: 14 }}>
            <label style={{ display: 'block', fontSize: 13, fontWeight: 600, marginBottom: 4 }}>Komponen Bonus</label>
            <select value={selectedWC} onChange={(e) => setSelectedWC(e.target.value)}
              style={{ width: '100%', padding: 8, borderRadius: 6, border: '1px solid #ccd', fontSize: 14 }}>
              <option value="">— Pilih komponen bonus —</option>
              {bonusComponents.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
            </select>
          </div>

          <div style={{ marginBottom: 14 }}>
            <label style={{ display: 'block', fontSize: 13, fontWeight: 600, marginBottom: 4 }}>Total Pot (Rp)</label>
            <input type="number" min="0" step="1000" value={pot} onChange={(e) => setPot(e.target.value)}
              placeholder="Masukkan total dana bonus"
              style={{ width: '100%', padding: 8, borderRadius: 6, border: '1px solid #ccd', fontSize: 14 }} />
          </div>

          {selectedWC && potNum > 0 && checkedIds.length > 0 && (
            <div style={{ background: '#f0f7ff', border: '1px solid #bdd7f9', borderRadius: 8, padding: '10px 14px', marginBottom: 14, fontSize: 14 }}>
              <span style={{ color: '#667' }}>{fmtIDR(potNum)} ÷ {checkedIds.length} karyawan = </span>
              <strong style={{ color: '#1967d2', fontSize: 16 }}>{fmtIDR(perEmployee)}</strong>
              <span style={{ color: '#667' }}> / karyawan</span>
            </div>
          )}

          {selectedWC && (
            <div>
              <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 8 }}>
                <div style={{ fontSize: 13, fontWeight: 600, color: '#334' }}>
                  {loadingEligible ? 'Memuat…' : `${eligible.length} karyawan eligible`}
                </div>
                {eligible.length > 0 && (
                  <button onClick={toggleAll} style={{ background: 'none', border: '1px solid #ccd', borderRadius: 6, padding: '4px 10px', fontSize: 12, cursor: 'pointer' }}>
                    {eligible.every((r) => checked[r.line_component_id]) ? 'Batal Pilih Semua' : 'Pilih Semua'}
                  </button>
                )}
              </div>

              {!loadingEligible && eligible.length === 0 && (
                <div style={{ color: '#889', fontSize: 13, padding: '8px 0' }}>Tidak ada karyawan dengan komponen bonus ini pada periode ini.</div>
              )}

              {eligible.map((row) => (
                <label key={row.line_component_id} style={{ display: 'flex', alignItems: 'center', gap: 10, padding: '8px 10px', borderRadius: 6, cursor: 'pointer', background: checked[row.line_component_id] ? '#f5f9ff' : '#fff', border: '1px solid', borderColor: checked[row.line_component_id] ? '#bdd7f9' : '#e6e8ee', marginBottom: 6 }}>
                  <input type="checkbox" checked={!!checked[row.line_component_id]} onChange={() => toggle(row.line_component_id)}
                    style={{ width: 16, height: 16, flexShrink: 0 }} />
                  <div style={{ flex: 1, minWidth: 0 }}>
                    <div style={{ fontWeight: 600, fontSize: 14 }}>{row.employee_name}</div>
                    <div style={{ fontSize: 12, color: '#889' }}>{row.employee_code}</div>
                  </div>
                  <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexShrink: 0 }}>
                    <ScoreBadge score={row.performance_score} />
                    {row.reviewed && <span style={{ fontSize: 11, color: '#1e7e34', background: '#e6f4ea', padding: '2px 6px', borderRadius: 4 }}>✓ Reviewed</span>}
                  </div>
                </label>
              ))}
            </div>
          )}
        </div>

        <div style={{ padding: '12px 20px', borderTop: '1px solid #e6e8ee', display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
          <button onClick={onClose} disabled={busy}
            style={{ background: '#fff', border: '1px solid #ccd', borderRadius: 8, padding: '10px 16px', cursor: 'pointer' }}>
            Batal
          </button>
          <button onClick={apply} disabled={busy || !selectedWC || checkedIds.length === 0 || perEmployee <= 0}
            style={{ background: (selectedWC && checkedIds.length > 0 && perEmployee > 0) ? '#e37400' : '#ccc', color: '#fff', border: 0, borderRadius: 8, padding: '10px 20px', fontWeight: 600, cursor: (selectedWC && checkedIds.length > 0 && perEmployee > 0) ? 'pointer' : 'not-allowed' }}>
            {busy ? 'Menerapkan…' : `Terapkan ke ${checkedIds.length} Karyawan`}
          </button>
        </div>
      </div>
    </div>
  );
}
