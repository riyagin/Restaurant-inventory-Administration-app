import { useEffect, useState, useCallback } from 'react';
import { Link } from 'react-router-dom';
import { getItems, deleteItem } from '../api';

function UnitChain({ units }) {
  return (
    <span style={{fontSize:'0.85rem'}}>
      {units.map((u, i) => (
        <span key={i}>
          {i > 0 && (
            <span style={{color:'#aaa',margin:'0 4px'}}>
              → <span style={{color:'#888',fontSize:'0.75rem'}}>×{u.perPrev}</span>
            </span>
          )}
          <span className="badge">{u.name}</span>
        </span>
      ))}
    </span>
  );
}

export default function Items() {
  const [items, setItems] = useState([]);
  const [search, setSearch] = useState('');
  const [typeFilter, setTypeFilter] = useState('');

  const load = useCallback(() => {
    const params = { search };
    if (typeFilter !== '') params.is_stock = typeFilter;
    getItems(params).then(r => setItems(r.data));
  }, [search, typeFilter]);

  useEffect(() => { load(); }, [load]);

  const handleDelete = async (id) => {
    if (!confirm('Yakin hapus barang ini? Semua catatan inventaris terkait juga akan dihapus.')) return;
    try {
      await deleteItem(id);
      load();
    } catch (err) {
      alert(err.response?.data?.error || 'Gagal menghapus barang.');
    }
  };

  return (
    <>
      <div className="page-header">
        <h1>Barang</h1>
        <Link to="/items/new" className="btn btn-primary">+ Tambah Barang</Link>
      </div>

      <div className="card">
        <div className="card-header">
          <h2>{items.length} item{items.length !== 1 ? 's' : ''}</h2>
          <div className="filters">
            <input
              placeholder="Cari nama atau kode..."
              value={search}
              onChange={e => setSearch(e.target.value)}
            />
            <select value={typeFilter} onChange={e => setTypeFilter(e.target.value)}>
              <option value="">Semua Tipe</option>
              <option value="true">Barang Stok</option>
              <option value="false">Barang Non-Stok</option>
            </select>
          </div>
        </div>

        <table>
          <thead>
            <tr>
              <th>Nama</th>
              <th>Kode</th>
              <th>Tipe</th>
              <th>Satuan</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {items.length === 0 ? (
              <tr><td colSpan={5} style={{textAlign:'center',color:'#999',padding:'2rem'}}>Tidak ada data</td></tr>
            ) : items.map(item => (
              <tr key={item.id}>
                <td style={{fontWeight: 500}}>
                  {item.is_stock === false ? (
                    <Link to={`/items/history/${item.id}`} style={{color:'#4f8ef7',textDecoration:'none'}}>{item.name}</Link>
                  ) : item.name}
                </td>
                <td style={{color:'#888',fontSize:'0.85rem'}}>{item.code}</td>
                <td>
                  {item.is_stock === false ? (
                    <span className="badge" style={{background:'#fff3e0',color:'#f57c00'}}>Non-Stok</span>
                  ) : (
                    <span className="badge" style={{background:'#e8f5e9',color:'#388e3c'}}>Stok</span>
                  )}
                </td>
                <td><UnitChain units={item.units} /></td>
                <td>
                  <div className="actions">
                    {item.is_stock === false && (
                      <Link to={`/items/history/${item.id}`} className="btn btn-secondary btn-sm">Riwayat</Link>
                    )}
                    <Link to={`/items/edit/${item.id}`} className="btn btn-secondary btn-sm">Edit</Link>
                    <button onClick={() => handleDelete(item.id)} className="btn btn-danger btn-sm">Hapus</button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </>
  );
}
