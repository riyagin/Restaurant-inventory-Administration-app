import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { getStats, getInventory } from '../api';

const idr = (v) =>
  new Intl.NumberFormat('id-ID', { style: 'currency', currency: 'IDR', maximumFractionDigits: 0 }).format(v);

export default function Dashboard() {
  const [stats, setStats] = useState(null);
  const [recent, setRecent] = useState([]);

  useEffect(() => {
    getStats().then(r => setStats(r.data));
    getInventory().then(r => setRecent(r.data.slice(0, 5)));
  }, []);

  if (!stats) return <p>Loading...</p>;

  return (
    <>
      <div className="page-header">
        <h1>Dashboard</h1>
      </div>

      <div className="stats-grid">
        <div className="stat-card">
          <div className="label">Total Products</div>
          <div className="value">{stats.totalItems}</div>
        </div>
        <div className="stat-card">
          <div className="label">Inventory Records</div>
          <div className="value">{stats.totalInventoryRecords}</div>
        </div>
        <div className="stat-card">
          <div className="label">Total Value</div>
          <div className="value" style={{fontSize:'1.3rem'}}>{idr(stats.totalValue)}</div>
        </div>
      </div>

      {recent.length > 0 && (
        <div className="card">
          <div className="card-header">
            <h2>Recent Inventory</h2>
            <Link to="/inventory" className="btn btn-secondary btn-sm">View All</Link>
          </div>
          <table>
            <thead>
              <tr>
                <th>Item</th>
                <th>Quantity</th>
                <th>Brand</th>
                <th>Warehouse</th>
                <th>Value</th>
              </tr>
            </thead>
            <tbody>
              {recent.map(rec => (
                <tr key={rec.id}>
                  <td>{rec.itemName}</td>
                  <td>{rec.quantity} {rec.unitName}</td>
                  <td>{rec.brand}</td>
                  <td>{rec.warehouse}</td>
                  <td>{idr(rec.value)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </>
  );
}
