import { useEffect, useState } from 'react';
import { useNavigate, useParams, Link } from 'react-router-dom';
import { getItems, getWarehouses, getInventoryRecord, createInventoryRecord, updateInventoryRecord } from '../api';

const idrFormat = (v) => new Intl.NumberFormat('id-ID').format(v);
const today = new Date().toISOString().split('T')[0];

const empty = { item_id: '', warehouse_id: '', quantity: '', unit_index: '0', value: '', date: today };

export default function InventoryForm() {
  const { id } = useParams();
  const navigate = useNavigate();
  const [form, setForm] = useState(empty);
  const [items, setItems] = useState([]);
  const [warehouses, setWarehouses] = useState([]);
  const [error, setError] = useState('');
  const isEdit = Boolean(id);

  useEffect(() => {
    getItems().then(r => setItems(r.data));
    getWarehouses().then(r => setWarehouses(r.data));
    if (id) {
      getInventoryRecord(id).then(r => setForm({
        ...r.data,
        unit_index: String(r.data.unit_index),
        date: r.data.date?.split('T')[0] ?? today,
      }));
    }
  }, [id]);

  const set = (field) => (e) => {
    const val = e.target.value;
    setForm(f => {
      const updated = { ...f, [field]: val };
      if (field === 'item_id') {
        const selected = items.find(i => i.id === val);
        updated.unit_index = selected ? String(selected.units.length - 1) : '0';
      }
      return updated;
    });
  };

  const selectedItem = items.find(i => i.id === form.item_id);

  const handleSubmit = async (e) => {
    e.preventDefault();
    setError('');
    const payload = {
      ...form,
      quantity: Number(form.quantity),
      unit_index: Number(form.unit_index),
      value: Number(form.value),
    };
    try {
      if (isEdit) await updateInventoryRecord(id, payload);
      else await createInventoryRecord(payload);
      navigate('/inventory');
    } catch (err) {
      setError(err.response?.data?.error || 'Something went wrong');
    }
  };

  return (
    <div className="card form-card">
      <h2>{isEdit ? 'Edit Inventory Record' : 'Add Inventory Record'}</h2>
      {error && <div className="error-msg">{error}</div>}
      <form onSubmit={handleSubmit}>
        <div className="form-group">
          <label>Item</label>
          <select value={form.item_id} onChange={set('item_id')} required>
            <option value="">Select an item...</option>
            {items.map(item => (
              <option key={item.id} value={item.id}>{item.name} ({item.code})</option>
            ))}
          </select>
        </div>

        <div className="form-row">
          <div className="form-group">
            <label>Quantity</label>
            <input type="number" min="0" value={form.quantity} onChange={set('quantity')} required placeholder="0" />
          </div>
          <div className="form-group">
            <label>Unit</label>
            <select value={form.unit_index} onChange={set('unit_index')} disabled={!selectedItem}>
              {selectedItem
                ? selectedItem.units.map((u, i) => <option key={i} value={String(i)}>{u.name}</option>)
                : <option value="0">Select item first</option>
              }
            </select>
          </div>
        </div>

        <div className="form-group">
          <label>Warehouse</label>
          <select value={form.warehouse_id} onChange={set('warehouse_id')} required>
            <option value="">Select a warehouse...</option>
            {warehouses.map(w => <option key={w.id} value={w.id}>{w.name}</option>)}
          </select>
        </div>

        <div className="form-row">
          <div className="form-group">
            <label>Value (Rp)</label>
            <input type="number" min="0" value={form.value} onChange={set('value')} required placeholder="0" />
            {form.value > 0 && (
              <small style={{color:'#888',marginTop:'0.25rem',display:'block'}}>
                Rp {idrFormat(Number(form.value))}
              </small>
            )}
          </div>
          <div className="form-group">
            <label>Date</label>
            <input type="date" value={form.date} onChange={set('date')} required />
          </div>
        </div>

        <div className="form-actions">
          <button type="submit" className="btn btn-primary">{isEdit ? 'Save Changes' : 'Add Record'}</button>
          <Link to="/inventory" className="btn btn-secondary">Cancel</Link>
        </div>
      </form>
    </div>
  );
}
