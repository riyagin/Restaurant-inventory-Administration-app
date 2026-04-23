import { useEffect, useRef, useState } from 'react';
import { useNavigate, useParams, Link } from 'react-router-dom';
import { getItems, getWarehouses, getVendors, getAccounts, getBranches, getDivisions, getInvoice, createInvoice, updateInvoice, uploadInvoicePhoto, getItemLastPrice } from '../api';

const idr = (v) =>
  new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(v);

const today = new Date().toISOString().split('T')[0];

const emptyPurchaseRow = () => ({ item_id: '', quantity: '', unit_index: '0', price: '' });
const emptyExpenseRow  = () => ({ item_id: '', quantity: '', unit_index: '0', price: '' });

const emptyHeader = { date: today, warehouse_id: '', payment_status: 'unpaid', account_id: '', branch_id: '', division_id: '', vendor_id: '', reference_number: '' };

export default function InvoiceForm() {
  const { id } = useParams();
  const navigate = useNavigate();
  const isEdit = Boolean(id);
  const fileInputRef = useRef(null);

  const [invoiceType, setInvoiceType] = useState('purchase');
  const [header, setHeader] = useState(emptyHeader);
  const [rows, setRows] = useState([emptyPurchaseRow()]);
  const [items, setItems] = useState([]);
  const [nonStockItems, setNonStockItems] = useState([]);
  const [warehouses, setWarehouses] = useState([]);
  const [vendors, setVendors] = useState([]);
  const [accounts, setAccounts] = useState([]);
  const [branches, setBranches] = useState([]);
  const [divisions, setDivisions] = useState([]);
  const [lastPrices, setLastPrices] = useState({});  // key: `${item_id}:${unit_index}` → price
  const [photoFile, setPhotoFile] = useState(null);
  const [existingPhoto, setExistingPhoto] = useState(null);
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    getItems({ is_stock: 'true' }).then(r => setItems(r.data));
    getItems({ is_stock: 'false' }).then(r => setNonStockItems(r.data));
    getWarehouses().then(r => setWarehouses(r.data));
    getVendors().then(r => setVendors(r.data));
    getAccounts().then(r => setAccounts(r.data));
    getBranches().then(r => setBranches(r.data));
    if (id) {
      getInvoice(id).then(r => {
        const inv = r.data;
        const type = inv.invoice_type ?? 'purchase';
        setInvoiceType(type);
        setExistingPhoto(inv.photo_path ?? null);
        if (inv.branch_id) getDivisions({ branch_id: inv.branch_id }).then(r => setDivisions(r.data));
        setHeader({
          date: inv.date?.split('T')[0] ?? today,
          warehouse_id: inv.warehouse_id ?? '',
          payment_status: inv.payment_status,
          account_id: inv.account_id ?? '',
          branch_id: inv.branch_id ?? '',
          division_id: inv.division_id ?? '',
          vendor_id: inv.vendor_id ?? '',
          reference_number: inv.reference_number ?? '',
        });
        if (type === 'expense') {
          setRows(inv.items.map(i => i.item_id ? ({
            item_id: i.item_id,
            quantity: String(i.quantity),
            unit_index: String(i.unit_index ?? 0),
            price: String(i.price),
          }) : ({
            item_id: '',
            description: i.description ?? '',
            quantity: String(i.quantity),
            unit_index: '0',
            price: String(i.price),
          })));
        } else {
          setRows(inv.items.map(i => ({
            item_id: i.item_id,
            quantity: String(i.quantity),
            unit_index: String(i.unit_index),
            price: String(i.price),
          })));
        }
      });
    }
  }, [id]);

  const switchType = (type) => {
    setInvoiceType(type);
    setRows(type === 'expense' ? [emptyExpenseRow()] : [emptyPurchaseRow()]);
  };

  const setHeaderField = (field) => (e) => {
    const val = e.target.value;
    setHeader(h => ({ ...h, [field]: val, ...(field === 'branch_id' ? { division_id: '' } : {}) }));
    if (field === 'branch_id') {
      setDivisions([]);
      if (val) getDivisions({ branch_id: val }).then(r => setDivisions(r.data));
    }
  };

  const fetchLastPrice = (itemId, unitIndex, rowIndex) => {
    if (!itemId) return;
    const key = `${itemId}:${unitIndex}`;
    if (lastPrices[key] !== undefined) {
      setRows(rs => rs.map((r, i) => i === rowIndex && !r.price ? { ...r, price: String(lastPrices[key]) } : r));
      return;
    }
    getItemLastPrice(itemId, { unit_index: unitIndex }).then(res => {
      const price = res.data?.price ?? null;
      setLastPrices(p => ({ ...p, [key]: price }));
      if (price != null) {
        setRows(rs => rs.map((r, i) => i === rowIndex && !r.price ? { ...r, price: String(price) } : r));
      }
    });
  };

  const setPurchaseRow = (index, field) => (e) => {
    const val = e.target.value;
    setRows(rs => rs.map((r, i) => {
      if (i !== index) return r;
      const updated = { ...r, [field]: val };
      if (field === 'item_id') {
        const selected = items.find(it => it.id === val);
        updated.unit_index = selected ? String(selected.units.length - 1) : '0';
        updated.price = '';
        if (val) fetchLastPrice(val, updated.unit_index, index);
      }
      if (field === 'unit_index' && r.item_id) {
        updated.price = '';
        fetchLastPrice(r.item_id, val, index);
      }
      return updated;
    }));
  };

  const setExpenseRow = (index, field) => (e) => {
    const val = e.target.value;
    setRows(rs => rs.map((r, i) => {
      if (i !== index) return r;
      const updated = { ...r, [field]: val };
      if (field === 'item_id') {
        const selected = nonStockItems.find(it => it.id === val);
        updated.unit_index = selected ? String(selected.units.length - 1) : '0';
        updated.price = '';
        if (val) fetchLastPrice(val, updated.unit_index, index);
      }
      if (field === 'unit_index' && r.item_id) {
        updated.price = '';
        fetchLastPrice(r.item_id, val, index);
      }
      return updated;
    }));
  };

  const addRow = () => setRows(rs => [...rs, invoiceType === 'expense' ? emptyExpenseRow() : emptyPurchaseRow()]);
  const removeRow = (index) => setRows(rs => rs.filter((_, i) => i !== index));

  const rowTotal = (row) => {
    const qty = Number(row.quantity);
    const price = Number(row.price);
    return isNaN(qty) || isNaN(price) ? 0 : qty * price;
  };
  const grandTotal = rows.reduce((sum, r) => sum + rowTotal(r), 0);

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError('');

    if (invoiceType === 'purchase') {
      for (const [i, row] of rows.entries()) {
        if (!row.item_id || !row.quantity || !row.price) {
          setError(`Row ${i + 1}: item, quantity, and price are required`);
          return;
        }
      }
    } else {
      for (const [i, row] of rows.entries()) {
        if (!row.item_id && !row.description) {
          setError(`Row ${i + 1}: item selection is required`);
          return;
        }
        if (!row.quantity || !row.price) {
          setError(`Row ${i + 1}: quantity and price are required`);
          return;
        }
      }
    }

    const needsAccount = header.payment_status !== 'unpaid';
    const payload = {
      date: header.date,
      payment_status: header.payment_status,
      account_id: needsAccount ? header.account_id || null : null,
      invoice_type: invoiceType,
      warehouse_id: invoiceType === 'purchase' ? header.warehouse_id : undefined,
      branch_id:   invoiceType === 'expense' ? header.branch_id   || null : undefined,
      division_id: invoiceType === 'expense' ? header.division_id || null : undefined,
      vendor_id:   invoiceType === 'purchase' ? header.vendor_id  || null : undefined,
      reference_number: header.reference_number || null,
      items: invoiceType === 'expense'
        ? rows.map(r => r.item_id
            ? { item_id: r.item_id, unit_index: Number(r.unit_index), quantity: Number(r.quantity), price: Number(r.price) }
            : { description: r.description, quantity: Number(r.quantity), price: Number(r.price) })
        : rows.map(r => ({ item_id: r.item_id, quantity: Number(r.quantity), unit_index: Number(r.unit_index), price: Number(r.price), vendor_id: header.vendor_id || null })),
    };

    setSaving(true);
    try {
      let savedId = id;
      if (isEdit) {
        await updateInvoice(id, payload);
      } else {
        const res = await createInvoice(payload);
        savedId = res.data.id;
      }
      if (photoFile && savedId) {
        await uploadInvoicePhoto(savedId, photoFile);
      }
      navigate('/invoices');
    } catch (err) {
      setError(err.response?.data?.error || 'Something went wrong');
    } finally {
      setSaving(false);
    }
  };

  const needsAccount = header.payment_status !== 'unpaid';

  return (
    <div className="card" style={{ maxWidth: '960px' }}>
      <h2 style={{ marginBottom: '1.5rem' }}>{isEdit ? 'Edit Invoice' : 'New Invoice'}</h2>
      {error && <div className="error-msg" style={{ marginBottom: '1rem' }}>{error}</div>}

      {/* Type toggle */}
      {!isEdit && (
        <div style={{ display: 'flex', gap: '0.5rem', marginBottom: '1.5rem' }}>
          {['purchase', 'expense'].map(type => (
            <button
              key={type}
              type="button"
              onClick={() => switchType(type)}
              style={{
                padding: '0.45rem 1.1rem', borderRadius: '6px', fontWeight: 600, fontSize: '0.9rem', cursor: 'pointer',
                border: invoiceType === type ? '2px solid #4f8ef7' : '2px solid #e0e0e0',
                background: invoiceType === type ? '#e8f0fe' : '#f9f9f9',
                color: invoiceType === type ? '#4f8ef7' : '#666',
              }}
            >
              {type === 'purchase' ? 'Purchase (adds stock)' : 'Expense (no stock)'}
            </button>
          ))}
        </div>
      )}

      <form onSubmit={handleSubmit}>
        {/* Header fields */}
        <div className="form-row" style={{ marginBottom: '1rem' }}>
          <div className="form-group" style={{ margin: 0 }}>
            <label>Tanggal</label>
            <input type="date" value={header.date} onChange={setHeaderField('date')} required />
          </div>
          {invoiceType === 'purchase' && (
            <div className="form-group" style={{ margin: 0 }}>
              <label>Gudang</label>
              <select value={header.warehouse_id} onChange={setHeaderField('warehouse_id')} required>
                <option value="">Pilih gudang...</option>
                {warehouses.map(w => <option key={w.id} value={w.id}>{w.name}</option>)}
              </select>
            </div>
          )}
        </div>

        <div className="form-row" style={{ marginBottom: '1rem' }}>
          <div className="form-group" style={{ margin: 0 }}>
            <label>Status Pembayaran</label>
            <select value={header.payment_status} onChange={setHeaderField('payment_status')}>
              <option value="unpaid">Belum Dibayar</option>
              <option value="partial">Sebagian</option>
              <option value="paid">Lunas</option>
            </select>
          </div>
          {needsAccount && (
            <div className="form-group" style={{ margin: 0 }}>
              <label>Akun Pembayaran</label>
              <select value={header.account_id} onChange={setHeaderField('account_id')} required>
                <option value="">Pilih akun...</option>
                {accounts.map(a => <option key={a.id} value={a.id}>{a.name}</option>)}
              </select>
            </div>
          )}
          {invoiceType === 'purchase' && (
            <div className="form-group" style={{ margin: 0 }}>
              <label>Vendor <span style={{ color: '#aaa', fontWeight: 400 }}>(opsional)</span></label>
              <select value={header.vendor_id} onChange={setHeaderField('vendor_id')}>
                <option value="">Pilih vendor...</option>
                {vendors.map(v => <option key={v.id} value={v.id}>{v.name}</option>)}
              </select>
            </div>
          )}
          {invoiceType === 'expense' && (
            <>
              <div className="form-group" style={{ margin: 0 }}>
                <label>Branch</label>
                <select value={header.branch_id} onChange={setHeaderField('branch_id')}>
                  <option value="">Pilih branch...</option>
                  {branches.map(b => <option key={b.id} value={b.id}>{b.name}</option>)}
                </select>
              </div>
              <div className="form-group" style={{ margin: 0 }}>
                <label>Division</label>
                <select value={header.division_id} onChange={setHeaderField('division_id')} disabled={!header.branch_id}>
                  <option value="">Pilih division...</option>
                  {divisions.map(d => <option key={d.id} value={d.id}>{d.name}</option>)}
                </select>
              </div>
            </>
          )}
        </div>

        <div className="form-row" style={{ marginBottom: '1.5rem' }}>
          <div className="form-group" style={{ margin: 0 }}>
            <label>No. Referensi <span style={{ color: '#aaa', fontWeight: 400 }}>(opsional)</span></label>
            <input
              value={header.reference_number}
              onChange={setHeaderField('reference_number')}
              placeholder="e.g. nomor faktur eksternal, PO number..."
            />
          </div>
        </div>

        {/* Line items */}
        <div style={{ marginBottom: '1rem', fontWeight: 600, fontSize: '0.9rem', color: '#444' }}>
          {invoiceType === 'expense' ? 'Item Pengeluaran' : 'Item Pembelian'}
        </div>
        <div style={{ overflowX: 'auto', marginBottom: '0.5rem' }}>
          <table className="invoice-items-table">
            <thead>
              <tr>
                {invoiceType === 'expense' ? (
                  <>
                    <th>Item</th>
                    <th>Qty</th>
                    <th>Satuan</th>
                    <th>Harga / Satuan (Rp)</th>
                    <th>Subtotal</th>
                    <th></th>
                  </>
                ) : (
                  <>
                    <th>Item</th>
                    <th>Qty</th>
                    <th>Satuan</th>
                    <th>Harga / Satuan (Rp)</th>
                    <th>Subtotal</th>
                    <th></th>
                  </>
                )}
              </tr>
            </thead>
            <tbody>
              {rows.map((row, i) => invoiceType === 'expense' ? (
                <tr key={i}>
                  <td style={{ minWidth: '200px' }}>
                    {row.description && !row.item_id ? (
                      /* legacy row from dispatch-auto-generated invoice */
                      <input value={row.description} onChange={setExpenseRow(i, 'description')} style={{ width: '100%', color: '#888' }} />
                    ) : (
                      <select value={row.item_id} onChange={setExpenseRow(i, 'item_id')} required style={{ width: '100%' }}>
                        <option value="">Pilih item...</option>
                        {nonStockItems.map(it => <option key={it.id} value={it.id}>{it.name}</option>)}
                      </select>
                    )}
                  </td>
                  <td style={{ minWidth: '80px' }}>
                    <input type="number" min="0" step="any" value={row.quantity} onChange={setExpenseRow(i, 'quantity')} required placeholder="0" style={{ width: '100%' }} />
                  </td>
                  <td style={{ minWidth: '100px' }}>
                    {(() => {
                      const selectedItem = nonStockItems.find(it => it.id === row.item_id);
                      return (
                        <select value={row.unit_index} onChange={setExpenseRow(i, 'unit_index')} disabled={!selectedItem} style={{ width: '100%' }}>
                          {selectedItem
                            ? selectedItem.units.map((u, ui) => <option key={ui} value={String(ui)}>{u.name}</option>)
                            : <option value="0">—</option>}
                        </select>
                      );
                    })()}
                  </td>
                  <td style={{ minWidth: '140px' }}>
                    {(() => {
                      const key = `${row.item_id}:${row.unit_index}`;
                      const lp = row.item_id ? lastPrices[key] : undefined;
                      return (
                        <>
                          <input type="number" min="0" value={row.price} onChange={setExpenseRow(i, 'price')} required placeholder="0" style={{ width: '100%' }} />
                          {lp != null && (
                            <div style={{ fontSize: '0.72rem', color: '#888', marginTop: '0.2rem' }}>
                              Last: {idr(lp)}
                            </div>
                          )}
                        </>
                      );
                    })()}
                  </td>
                  <td style={{ minWidth: '120px', textAlign: 'right', fontWeight: 600, whiteSpace: 'nowrap' }}>{idr(rowTotal(row))}</td>
                  <td>{rows.length > 1 && <button type="button" onClick={() => removeRow(i)} className="btn btn-danger btn-sm">✕</button>}</td>
                </tr>
              ) : (
                <tr key={i}>
                  <td style={{ minWidth: '180px' }}>
                    <select value={row.item_id} onChange={setPurchaseRow(i, 'item_id')} required style={{ width: '100%' }}>
                      <option value="">Pilih item...</option>
                      {items.map(it => <option key={it.id} value={it.id}>{it.name}</option>)}
                    </select>
                  </td>
                  <td style={{ minWidth: '80px' }}>
                    <input type="number" min="1" value={row.quantity} onChange={setPurchaseRow(i, 'quantity')} required placeholder="0" style={{ width: '100%' }} />
                  </td>
                  <td style={{ minWidth: '100px' }}>
                    {(() => {
                      const selectedItem = items.find(it => it.id === row.item_id);
                      return (
                        <select value={row.unit_index} onChange={setPurchaseRow(i, 'unit_index')} disabled={!selectedItem} style={{ width: '100%' }}>
                          {selectedItem
                            ? selectedItem.units.map((u, ui) => <option key={ui} value={String(ui)}>{u.name}</option>)
                            : <option value="0">—</option>}
                        </select>
                      );
                    })()}
                  </td>
                  <td style={{ minWidth: '140px' }}>
                    {(() => {
                      const key = `${row.item_id}:${row.unit_index}`;
                      const lp = row.item_id ? lastPrices[key] : undefined;
                      return (
                        <>
                          <input type="number" min="0" value={row.price} onChange={setPurchaseRow(i, 'price')} required placeholder="0" style={{ width: '100%' }} />
                          {lp != null && (
                            <div style={{ fontSize: '0.72rem', color: '#888', marginTop: '0.2rem' }}>
                              Last: {idr(lp)}
                            </div>
                          )}
                        </>
                      );
                    })()}
                  </td>
                  <td style={{ minWidth: '120px', textAlign: 'right', fontWeight: 600, whiteSpace: 'nowrap' }}>{idr(rowTotal(row))}</td>
                  <td>{rows.length > 1 && <button type="button" onClick={() => removeRow(i)} className="btn btn-danger btn-sm">✕</button>}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>

        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '1.5rem' }}>
          <button type="button" onClick={addRow} className="btn btn-secondary">+ Tambah Baris</button>
          <div style={{ fontSize: '1.1rem', fontWeight: 700 }}>
            Total: <span style={{ color: '#4f8ef7', marginLeft: '0.5rem' }}>{idr(grandTotal)}</span>
          </div>
        </div>

        {/* Photo upload */}
        <div className="form-group">
          <label>Foto Bukti <span style={{ color: '#aaa', fontWeight: 400 }}>(opsional)</span></label>
          {existingPhoto && (
            <div style={{ marginBottom: '0.5rem' }}>
              <img
                src={`http://localhost:5000${existingPhoto}`}
                alt="Current receipt"
                style={{ maxHeight: '120px', maxWidth: '200px', borderRadius: '6px', border: '1px solid #e0e0e0', objectFit: 'contain' }}
              />
              <div style={{ fontSize: '0.78rem', color: '#888', marginTop: '0.25rem' }}>Current photo — upload a new one to replace it</div>
            </div>
          )}
          <input
            ref={fileInputRef}
            type="file"
            accept="image/*,.pdf"
            onChange={e => setPhotoFile(e.target.files[0] ?? null)}
          />
          {photoFile && (
            <div style={{ fontSize: '0.82rem', color: '#555', marginTop: '0.25rem' }}>
              Selected: {photoFile.name}
            </div>
          )}
        </div>

        <div className="form-actions">
          <button type="submit" className="btn btn-primary" disabled={saving}>
            {saving ? 'Menyimpan…' : isEdit ? 'Simpan Perubahan' : 'Buat Invoice'}
          </button>
          <Link to="/invoices" className="btn btn-secondary">Batal</Link>
        </div>
      </form>
    </div>
  );
}
