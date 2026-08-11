import { Fragment, useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import {
  getVendors, createVendor, updateVendor, deleteVendor,
  createVendorBankAccount, updateVendorBankAccount, deleteVendorBankAccount,
} from '../api';

// A vendor is paid by transfer, and one vendor routinely has more than one
// destination — a company account and the owner's personal one, a new number
// after a bank switch. The list therefore shows the default inline and expands
// to manage the rest, rather than pretending there is only ever one.

const emptyBank = {
  bank_name: '', account_number: '', account_holder: '',
  bank_branch: '', is_primary: false, note: '',
};

const inputStyle = {
  padding: '0.4rem 0.6rem', border: '1px solid #ddd',
  borderRadius: '6px', fontSize: '0.9rem', width: '100%',
};

export default function Vendors() {
  const [vendors, setVendors] = useState([]);
  const [newName, setNewName] = useState('');
  const [editId, setEditId] = useState(null);
  const [editName, setEditName] = useState('');
  const [error, setError] = useState('');

  // Which vendor's transfer accounts are open, and the row being edited inside.
  const [openId, setOpenId] = useState(null);
  const [bankForm, setBankForm] = useState(emptyBank);
  const [bankEditId, setBankEditId] = useState(null);
  const [bankError, setBankError] = useState('');
  const [saving, setSaving] = useState(false);

  const load = () => getVendors().then(r => setVendors(r.data));
  useEffect(() => { load(); }, []);

  const handleAdd = async (e) => {
    e.preventDefault();
    setError('');
    try {
      await createVendor({ name: newName });
      setNewName('');
      load();
    } catch (err) {
      setError(err.response?.data?.error || 'Terjadi kesalahan');
    }
  };

  const handleEdit = async (id) => {
    setError('');
    try {
      await updateVendor(id, { name: editName });
      setEditId(null);
      load();
    } catch (err) {
      setError(err.response?.data?.error || 'Terjadi kesalahan');
    }
  };

  const handleDelete = async (id) => {
    if (!confirm('Yakin hapus vendor ini?')) return;
    setError('');
    try {
      await deleteVendor(id);
      load();
    } catch (err) {
      setError(err.response?.data?.error || 'Tidak bisa dihapus: vendor sedang digunakan');
    }
  };

  const startEdit = (v) => { setEditId(v.id); setEditName(v.name); };

  const resetBankForm = () => { setBankForm(emptyBank); setBankEditId(null); setBankError(''); };

  const toggleBanks = (id) => {
    resetBankForm();
    setOpenId(openId === id ? null : id);
  };

  const startBankEdit = (b) => {
    setBankEditId(b.id);
    setBankError('');
    setBankForm({
      bank_name: b.bank_name, account_number: b.account_number,
      account_holder: b.account_holder, bank_branch: b.bank_branch,
      is_primary: b.is_primary, note: b.note,
    });
  };

  const submitBank = async (e, vendorId) => {
    e.preventDefault();
    setBankError('');
    setSaving(true);
    try {
      if (bankEditId) await updateVendorBankAccount(vendorId, bankEditId, bankForm);
      else await createVendorBankAccount(vendorId, bankForm);
      resetBankForm();
      await load();
    } catch (err) {
      setBankError(err.response?.data?.error || 'Gagal menyimpan rekening');
    } finally {
      setSaving(false);
    }
  };

  const removeBank = async (vendorId, bankId) => {
    if (!confirm('Hapus rekening ini?')) return;
    setBankError('');
    try {
      await deleteVendorBankAccount(vendorId, bankId);
      if (bankEditId === bankId) resetBankForm();
      await load();
    } catch (err) {
      setBankError(err.response?.data?.error || 'Gagal menghapus rekening');
    }
  };

  return (
    <>
      <div className="page-header">
        <h1>Vendor</h1>
      </div>

      <div className="card" style={{maxWidth:'900px'}}>
        {error && <div className="error-msg" style={{marginBottom:'1rem'}}>{error}</div>}

        <form onSubmit={handleAdd} style={{display:'flex',gap:'0.75rem',marginBottom:'1.5rem',maxWidth:'560px'}}>
          <input
            style={{flex:1,padding:'0.55rem 0.75rem',border:'1px solid #ddd',borderRadius:'6px',fontSize:'0.95rem'}}
            placeholder="Nama vendor baru..."
            value={newName}
            onChange={e => setNewName(e.target.value)}
            required
          />
          <button type="submit" className="btn btn-primary">Tambah</button>
        </form>

        <table>
          <thead>
            <tr><th>Nama Vendor</th><th>Rekening Transfer</th><th></th></tr>
          </thead>
          <tbody>
            {vendors.length === 0 ? (
              <tr><td colSpan={3} style={{textAlign:'center',color:'#999',padding:'2rem'}}>Belum ada vendor</td></tr>
            ) : vendors.map(v => {
              const banks = v.bank_accounts || [];
              const primary = banks.find(b => b.is_primary);
              const isOpen = openId === v.id;
              return (
                <Fragment key={v.id}>
                  <tr>
                    <td>
                      {editId === v.id ? (
                        <input
                          value={editName}
                          onChange={e => setEditName(e.target.value)}
                          style={{...inputStyle, border:'1px solid #4f8ef7'}}
                          autoFocus
                        />
                      ) : v.name}
                    </td>
                    <td>
                      {primary ? (
                        <span style={{fontSize:'0.88rem'}}>
                          <strong>{primary.bank_name}</strong> {primary.account_number}
                          {primary.account_holder && <span style={{color:'#777'}}> · {primary.account_holder}</span>}
                          {banks.length > 1 && (
                            <span style={{color:'#777'}}> (+{banks.length - 1} lainnya)</span>
                          )}
                        </span>
                      ) : (
                        <span style={{color:'#999',fontSize:'0.88rem'}}>Belum ada rekening</span>
                      )}
                    </td>
                    <td>
                      <div className="actions">
                        {editId === v.id ? (
                          <>
                            <button onClick={() => handleEdit(v.id)} className="btn btn-primary btn-sm">Simpan</button>
                            <button onClick={() => setEditId(null)} className="btn btn-secondary btn-sm">Batal</button>
                          </>
                        ) : (
                          <>
                            <button onClick={() => toggleBanks(v.id)} className="btn btn-secondary btn-sm">
                              {isOpen ? 'Tutup' : 'Rekening'}
                            </button>
                            <Link to={`/vendors/${v.id}/history`} className="btn btn-secondary btn-sm">Riwayat</Link>
                            <button onClick={() => startEdit(v)} className="btn btn-secondary btn-sm">Edit</button>
                            <button onClick={() => handleDelete(v.id)} className="btn btn-danger btn-sm">Hapus</button>
                          </>
                        )}
                      </div>
                    </td>
                  </tr>

                  {isOpen && (
                    <tr>
                      <td colSpan={3} style={{background:'#fafbfc',padding:'1rem'}}>
                        {bankError && <div className="error-msg" style={{marginBottom:'0.75rem'}}>{bankError}</div>}

                        {banks.length > 0 && (
                          <table style={{marginBottom:'1rem'}}>
                            <thead>
                              <tr>
                                <th>Bank</th><th>Nomor Rekening</th><th>Atas Nama</th>
                                <th>Cabang</th><th>Catatan</th><th></th>
                              </tr>
                            </thead>
                            <tbody>
                              {banks.map(b => (
                                <tr key={b.id}>
                                  <td>
                                    {b.bank_name}
                                    {b.is_primary && (
                                      <span style={{marginLeft:'0.4rem',fontSize:'0.72rem',background:'#e7f0ff',
                                                    color:'#2c6ad4',padding:'0.1rem 0.4rem',borderRadius:'4px'}}>
                                        Utama
                                      </span>
                                    )}
                                  </td>
                                  <td style={{fontFamily:'monospace'}}>{b.account_number}</td>
                                  <td>{b.account_holder || '—'}</td>
                                  <td>{b.bank_branch || '—'}</td>
                                  <td style={{color:'#777',fontSize:'0.85rem'}}>{b.note || '—'}</td>
                                  <td>
                                    <div className="actions">
                                      <button onClick={() => startBankEdit(b)} className="btn btn-secondary btn-sm">Edit</button>
                                      <button onClick={() => removeBank(v.id, b.id)} className="btn btn-danger btn-sm">Hapus</button>
                                    </div>
                                  </td>
                                </tr>
                              ))}
                            </tbody>
                          </table>
                        )}

                        <form onSubmit={e => submitBank(e, v.id)}>
                          <div style={{display:'grid',gridTemplateColumns:'repeat(auto-fit,minmax(160px,1fr))',
                                       gap:'0.6rem',marginBottom:'0.6rem'}}>
                            <input style={inputStyle} placeholder="Bank (mis. BCA)" required
                                   value={bankForm.bank_name}
                                   onChange={e => setBankForm({...bankForm, bank_name: e.target.value})} />
                            <input style={inputStyle} placeholder="Nomor rekening" required
                                   value={bankForm.account_number}
                                   onChange={e => setBankForm({...bankForm, account_number: e.target.value})} />
                            <input style={inputStyle} placeholder="Atas nama"
                                   value={bankForm.account_holder}
                                   onChange={e => setBankForm({...bankForm, account_holder: e.target.value})} />
                            <input style={inputStyle} placeholder="Cabang / KCP"
                                   value={bankForm.bank_branch}
                                   onChange={e => setBankForm({...bankForm, bank_branch: e.target.value})} />
                            <input style={inputStyle} placeholder="Catatan"
                                   value={bankForm.note}
                                   onChange={e => setBankForm({...bankForm, note: e.target.value})} />
                          </div>
                          <div style={{display:'flex',alignItems:'center',gap:'1rem',flexWrap:'wrap'}}>
                            <label style={{display:'flex',alignItems:'center',gap:'0.4rem',fontSize:'0.88rem'}}>
                              <input type="checkbox" checked={bankForm.is_primary}
                                     onChange={e => setBankForm({...bankForm, is_primary: e.target.checked})} />
                              Jadikan rekening utama
                            </label>
                            <button type="submit" className="btn btn-primary btn-sm" disabled={saving}>
                              {bankEditId ? 'Simpan Perubahan' : 'Tambah Rekening'}
                            </button>
                            {bankEditId && (
                              <button type="button" onClick={resetBankForm} className="btn btn-secondary btn-sm">Batal</button>
                            )}
                          </div>
                        </form>
                      </td>
                    </tr>
                  )}
                </Fragment>
              );
            })}
          </tbody>
        </table>
      </div>
    </>
  );
}
