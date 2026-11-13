import { useEffect, useState } from 'react';
import { getPerformanceGrades, savePerformanceGrades } from '../../api';

const fmtIDR = (n) => new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(n || 0);

// Example amount for the live preview column — just to make the percentages concrete.
const EXAMPLE = 300000;

// The company-wide grade scale: a monthly performance score of at least
// min_score earns pay_percent of every grade-linked wage component. Exactly one
// tier applies (the highest one reached). Saved as a whole; the server checks
// that there is a tier at 0 and that pay never rises as the score falls.
export default function GradeScaleEditor() {
  const [rows, setRows] = useState([]);
  const [saved, setSaved] = useState([]);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [saving, setSaving] = useState(false);

  const load = () => getPerformanceGrades()
    .then((r) => {
      const data = (r.data || []).map((g) => ({ label: g.label, min_score: String(g.min_score), pay_percent: String(g.pay_percent) }));
      setRows(data);
      setSaved(data);
    })
    .catch(() => setError('Gagal memuat skala grade'));
  useEffect(() => { load(); }, []);

  const dirty = JSON.stringify(rows) !== JSON.stringify(saved);
  const set = (i, key, value) => setRows((rs) => rs.map((r, j) => (j === i ? { ...r, [key]: value } : r)));
  const add = () => setRows((rs) => [...rs, { label: '', min_score: '', pay_percent: '' }]);
  const remove = (i) => setRows((rs) => rs.filter((_, j) => j !== i));

  // Score range each tier covers, for display ("90–100", "80–89", …).
  const ranges = (() => {
    const sorted = rows
      .map((r, i) => ({ i, min: Number(r.min_score) }))
      .filter((x) => x.min >= 0 && x.min <= 100 && rows[x.i].min_score !== '')
      .sort((a, b) => b.min - a.min);
    const out = {};
    sorted.forEach((x, k) => {
      const upper = k === 0 ? 100 : sorted[k - 1].min - 1;
      out[x.i] = upper < x.min ? '—' : (upper === x.min ? `${x.min}` : `${x.min}–${upper}`);
    });
    return out;
  })();

  const save = async () => {
    setError(''); setNotice('');
    for (const r of rows) {
      if (!r.label.trim() || r.min_score === '' || r.pay_percent === '') {
        setError('Setiap grade harus punya nama, skor minimum, dan persentase.');
        return;
      }
    }
    setSaving(true);
    try {
      await savePerformanceGrades(rows.map((r) => ({
        label: r.label.trim(), min_score: Number(r.min_score), pay_percent: Number(r.pay_percent),
      })));
      await load();
      setNotice('Skala grade disimpan. Berlaku untuk baris penggajian yang dibuat atau dibuat ulang setelah ini.');
    } catch (err) {
      setError(err.response?.data?.error || 'Gagal menyimpan skala grade');
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="card" style={{ maxWidth: 760, marginBottom: '1rem' }}>
      <div style={{ display: 'flex', alignItems: 'baseline', justifyContent: 'space-between', gap: '1rem', flexWrap: 'wrap' }}>
        <h2 style={{ margin: 0, fontSize: '1.05rem' }}>Skala Grade Kinerja</h2>
        <small style={{ color: '#8a93a6' }}>Satu skala untuk semua komponen yang ikut grade</small>
      </div>
      <p style={{ color: '#667', fontSize: '0.85rem', margin: '0.5rem 0 0.75rem' }}>
        Skor kinerja bulanan (100 − poin pelanggaran) menentukan grade karyawan. Komponen yang <strong>ikut grade</strong> dibayar
        sebesar persentase grade tersebut — bukan lagi semua-atau-tidak-sama-sekali. Yang dipakai hanya satu grade: grade tertinggi
        yang skornya tercapai. Bila skor bulan itu belum dievaluasi, komponen dibayar penuh.
      </p>

      {error && <div className="error-msg" style={{ marginBottom: '0.75rem' }}>{error}</div>}
      {notice && !dirty && <div style={{ marginBottom: '0.75rem', color: '#1e7e34', fontSize: '0.85rem' }}>{notice}</div>}

      <div style={{ overflowX: 'auto' }}>
        <table>
          <thead>
            <tr>
              <th>Grade</th><th>Skor Minimum</th><th>Rentang Skor</th><th>Dibayar</th>
              <th style={{ textAlign: 'right' }}>Contoh {fmtIDR(EXAMPLE)}</th><th></th>
            </tr>
          </thead>
          <tbody>
            {rows.length === 0 && (
              <tr><td colSpan={6} style={{ textAlign: 'center', color: '#999', padding: '1rem' }}>Belum ada grade</td></tr>
            )}
            {rows.map((r, i) => {
              const pct = Number(r.pay_percent);
              const example = r.pay_percent === '' || Number.isNaN(pct) ? null : Math.round((EXAMPLE * pct) / 100);
              return (
                <tr key={i}>
                  <td><input value={r.label} onChange={(e) => set(i, 'label', e.target.value)} placeholder="A" style={{ width: 70 }} /></td>
                  <td><input type="number" min="0" max="100" value={r.min_score} onChange={(e) => set(i, 'min_score', e.target.value)} style={{ width: 80 }} /></td>
                  <td style={{ color: '#667' }}>{ranges[i] ?? '—'}</td>
                  <td>
                    <span style={{ display: 'inline-flex', alignItems: 'center', gap: 4 }}>
                      <input type="number" min="0" max="100" value={r.pay_percent} onChange={(e) => set(i, 'pay_percent', e.target.value)} style={{ width: 70 }} />%
                    </span>
                  </td>
                  <td style={{ textAlign: 'right', fontVariantNumeric: 'tabular-nums' }}>{example == null ? '—' : fmtIDR(example)}</td>
                  <td><button type="button" onClick={() => remove(i)} className="btn btn-danger btn-sm" disabled={rows.length <= 1}>Hapus</button></td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>

      <div style={{ display: 'flex', gap: '0.5rem', marginTop: '0.75rem', flexWrap: 'wrap' }}>
        <button type="button" onClick={add} className="btn btn-secondary btn-sm">+ Tambah Grade</button>
        <span style={{ flex: 1 }} />
        {dirty && <button type="button" onClick={() => { setRows(saved); setError(''); }} className="btn btn-secondary btn-sm">Batalkan</button>}
        <button type="button" onClick={save} className="btn btn-primary btn-sm" disabled={!dirty || saving}>
          {saving ? 'Menyimpan...' : 'Simpan Skala'}
        </button>
      </div>
      <small style={{ display: 'block', marginTop: '0.5rem', color: '#8a93a6' }}>
        Harus ada grade dengan skor minimum 0. Baris penggajian yang sudah dibuat menyimpan grade-nya sendiri — buat ulang barisnya untuk memakai skala baru.
      </small>
    </div>
  );
}
