import { useEffect, useState, useCallback } from 'react';
import { getUser, getOvertimeRequests, createOvertimeRequest, deleteOvertimeRequest, getEmployees } from '../../api';

const fmtDate = (d) => d ? new Date(d).toLocaleDateString('id-ID', { day: '2-digit', month: 'short', year: 'numeric' }) : '-';

function currentMonthValue() {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`;
}

export default function OvertimePage() {
  const isAdmin = getUser()?.role === 'admin';

  const [requests, setRequests] = useState([]);
  const [employees, setEmployees] = useState([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);

  const [filterMonth, setFilterMonth] = useState(currentMonthValue());
  const [filterEmpId, setFilterEmpId] = useState('');

  const [showForm, setShowForm] = useState(false);
  const [form, setForm] = useState({ employee_id: '', date: '', hours: '', reason: '' });
  const [formError, setFormError] = useState('');

  const load = useCallback(async () => {
    setLoading(true); setError('');
    try {
      const { data } = await getOvertimeRequests({ month: filterMonth || undefined, employee_id: filterEmpId || undefined });
      setRequests(Array.isArray(data) ? data : []);
    } catch {
      setError('Gagal memuat data lembur');
    } finally {
      setLoading(false);
    }
  }, [filterMonth, filterEmpId]);

  useEffect(() => {
    getEmployees().then(r => setEmployees(r.data?.data || [])).catch(() => {});
  }, []);

  useEffect(() => { load(); }, [load]);

  const handleCreate = async (e) => {
    e.preventDefault();
    setFormError('');
    if (!form.employee_id) { setFormError('Pilih karyawan'); return; }
    if (!form.date) { setFormError('Masukkan tanggal'); return; }
    if (!form.hours || Number(form.hours) <= 0) { setFormError('Jam lembur harus lebih dari 0'); return; }
    setBusy(true);
    try {
      await createOvertimeRequest({ ...form, hours: Number(form.hours) });
      setForm({ employee_id: '', date: '', hours: '', reason: '' });
      setShowForm(false);
      await load();
    } catch (err) {
      setFormError(err?.response?.data?.error || 'Gagal menyimpan');
    } finally {
      setBusy(false);
    }
  };

  const handleDelete = async (id) => {
    if (!window.confirm('Hapus permintaan lembur ini?')) return;
    setBusy(true);
    try {
      await deleteOvertimeRequest(id);
      await load();
    } catch {
      setError('Gagal menghapus permintaan lembur');
    } finally {
      setBusy(false);
    }
  };

  const totalHours = requests.reduce((s, r) => s + Number(r.hours || 0), 0);

  return (
    <div style={{ padding: 24, maxWidth: 1100, margin: '0 auto' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 20, flexWrap: 'wrap', gap: 12 }}>
        <h1 style={{ margin: 0, fontSize: 24 }}>Permintaan Lembur</h1>
        {isAdmin && (
          <button onClick={() => { setShowForm(true); setFormError(''); }}
            style={{ background: '#1967d2', color: '#fff', border: 0, borderRadius: 8, padding: '10px 18px', fontWeight: 600, cursor: 'pointer' }}>
            + Tambah Lembur
          </button>
        )}
      </div>

      {error && <div style={{ background: '#fce8e6', color: '#c5221f', padding: 12, borderRadius: 8, marginBottom: 12 }}>{error}</div>}

      {/* Filters */}
      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', marginBottom: 16 }}>
        <input type="month" value={filterMonth} onChange={e => setFilterMonth(e.target.value)}
          style={{ padding: 8, borderRadius: 6, border: '1px solid #ccd' }} />
        <select value={filterEmpId} onChange={e => setFilterEmpId(e.target.value)}
          style={{ padding: 8, borderRadius: 6, border: '1px solid #ccd', minWidth: 200 }}>
          <option value="">Semua Karyawan</option>
          {employees.map(emp => <option key={emp.id} value={emp.id}>{emp.full_name} ({emp.employee_code})</option>)}
        </select>
      </div>

      {/* Summary chip */}
      {requests.length > 0 && (
        <div style={{ display: 'inline-flex', gap: 16, background: '#f6f7fa', borderRadius: 8, padding: '8px 16px', marginBottom: 16, fontSize: 14 }}>
          <span><strong>{requests.length}</strong> entri</span>
          <span><strong>{totalHours.toFixed(1)}</strong> jam total</span>
        </div>
      )}

      {/* Table */}
      <div style={{ background: '#fff', borderRadius: 10, boxShadow: '0 1px 3px rgba(0,0,0,.08)', overflowX: 'auto' }}>
        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 14 }}>
          <thead>
            <tr style={{ background: '#f1f3f7', textAlign: 'left' }}>
              <th style={{ padding: 10 }}>Karyawan</th>
              <th style={{ padding: 10 }}>Tanggal</th>
              <th style={{ padding: 10, textAlign: 'right' }}>Jam Lembur</th>
              <th style={{ padding: 10 }}>Keterangan</th>
              <th style={{ padding: 10 }}>Diinput Oleh</th>
              {isAdmin && <th style={{ padding: 10 }}></th>}
            </tr>
          </thead>
          <tbody>
            {loading ? (
              <tr><td colSpan={isAdmin ? 6 : 5} style={{ padding: 16, color: '#889' }}>Memuat…</td></tr>
            ) : requests.length === 0 ? (
              <tr><td colSpan={isAdmin ? 6 : 5} style={{ padding: 16, color: '#889' }}>Tidak ada data lembur.</td></tr>
            ) : requests.map(req => (
              <tr key={req.id} style={{ borderTop: '1px solid #eef0f4' }}>
                <td style={{ padding: 10 }}>
                  <div style={{ fontWeight: 600 }}>{req.employee_name}</div>
                  <div style={{ fontSize: 12, color: '#889' }}>{req.employee_code}</div>
                </td>
                <td style={{ padding: 10 }}>{fmtDate(req.date)}</td>
                <td style={{ padding: 10, textAlign: 'right', fontWeight: 600 }}>{Number(req.hours).toFixed(1)} jam</td>
                <td style={{ padding: 10, color: req.reason ? undefined : '#bbb' }}>{req.reason || '—'}</td>
                <td style={{ padding: 10, fontSize: 13, color: '#667' }}>{req.created_by_username || '—'}</td>
                {isAdmin && (
                  <td style={{ padding: 10 }}>
                    <button onClick={() => handleDelete(req.id)} disabled={busy}
                      style={{ background: 'none', border: '1px solid #e0b0b0', color: '#c5221f', borderRadius: 6, padding: '4px 10px', cursor: 'pointer', fontSize: 12 }}>
                      Hapus
                    </button>
                  </td>
                )}
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {/* Add form modal */}
      {showForm && (
        <div style={{ position: 'fixed', inset: 0, background: 'rgba(0,0,0,.4)', display: 'flex', alignItems: 'center', justifyContent: 'center', zIndex: 200 }}
          onClick={() => setShowForm(false)}>
          <div onClick={e => e.stopPropagation()}
            style={{ background: '#fff', borderRadius: 12, padding: 28, width: '100%', maxWidth: 460, boxShadow: '0 4px 24px rgba(0,0,0,.18)' }}>
            <h2 style={{ margin: '0 0 18px', fontSize: 18 }}>Tambah Permintaan Lembur</h2>
            {formError && <div style={{ background: '#fce8e6', color: '#c5221f', padding: 10, borderRadius: 6, marginBottom: 12, fontSize: 13 }}>{formError}</div>}
            <form onSubmit={handleCreate}>
              <label style={{ display: 'block', fontSize: 13, marginBottom: 4 }}>Karyawan</label>
              <select value={form.employee_id} onChange={e => setForm(f => ({ ...f, employee_id: e.target.value }))} required
                style={{ width: '100%', padding: 8, borderRadius: 6, border: '1px solid #ccd', marginBottom: 12 }}>
                <option value="">— Pilih Karyawan —</option>
                {employees.map(emp => <option key={emp.id} value={emp.id}>{emp.full_name} ({emp.employee_code})</option>)}
              </select>

              <label style={{ display: 'block', fontSize: 13, marginBottom: 4 }}>Tanggal Lembur</label>
              <input type="date" value={form.date} onChange={e => setForm(f => ({ ...f, date: e.target.value }))} required
                style={{ width: '100%', padding: 8, borderRadius: 6, border: '1px solid #ccd', marginBottom: 12 }} />

              <label style={{ display: 'block', fontSize: 13, marginBottom: 4 }}>Jumlah Jam Lembur</label>
              <input type="number" step="0.5" min="0.5" max="24" value={form.hours}
                onChange={e => setForm(f => ({ ...f, hours: e.target.value }))} required
                placeholder="mis. 2.5"
                style={{ width: '100%', padding: 8, borderRadius: 6, border: '1px solid #ccd', marginBottom: 12 }} />

              <label style={{ display: 'block', fontSize: 13, marginBottom: 4 }}>Keterangan (opsional)</label>
              <textarea value={form.reason} onChange={e => setForm(f => ({ ...f, reason: e.target.value }))}
                placeholder="Alasan atau pekerjaan yang dilakukan..."
                rows={2} style={{ width: '100%', padding: 8, borderRadius: 6, border: '1px solid #ccd', marginBottom: 16, resize: 'vertical' }} />

              <div style={{ display: 'flex', gap: 8 }}>
                <button type="submit" disabled={busy}
                  style={{ flex: 1, background: '#1967d2', color: '#fff', border: 0, borderRadius: 8, padding: '10px', fontWeight: 600, cursor: 'pointer' }}>
                  {busy ? 'Menyimpan…' : 'Simpan'}
                </button>
                <button type="button" onClick={() => setShowForm(false)}
                  style={{ flex: 1, background: '#fff', border: '1px solid #ccd', borderRadius: 8, padding: '10px', cursor: 'pointer' }}>
                  Batal
                </button>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
}
