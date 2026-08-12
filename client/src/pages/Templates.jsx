import { useEffect, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { getItems, getWarehouses, getVendors, getBranches } from '../api';
import DailyPurchasePanel from './templates/DailyPurchasePanel';
import InvoicePanel from './templates/InvoicePanel';
import DispatchPanel from './templates/DispatchPanel';

// One page for every kind of template. They were three separate pages under two
// different menus, which is a poor match for how they are used: a template is
// something you set up once and then revise occasionally, and when you revise
// one you are usually comparing it with the others — the same shopping run may
// exist as a daily purchase template and as an invoice template.
//
// The three lists are structurally different (an invoice template carries a
// type, a dispatch template a destination), so they stay three tables rather
// than being forced into one grid. What is shared is the chrome and the master
// data: items, warehouses, vendors and branches are fetched once here and handed
// down, instead of each panel refetching the same four lists.

const TABS = [
  ['pembelanjaan', 'Pembelanjaan Harian'],
  ['invoice',      'Invoice'],
  ['pengiriman',   'Pengiriman'],
];

const isTab = (k) => TABS.some(([key]) => key === k);

export default function Templates() {
  // The tab lives in the URL so a menu entry, a redirect from the old routes, or
  // a bookmark can open the page on the right list.
  const [params, setParams] = useSearchParams();
  const urlTab = params.get('tab');
  const tab = isTab(urlTab) ? urlTab : TABS[0][0];

  const [master, setMaster] = useState({ items: [], warehouses: [], vendors: [], branches: [] });
  const [counts, setCounts] = useState({});
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    Promise.all([getItems(), getWarehouses(), getVendors(), getBranches()])
      .then(([i, w, v, b]) => setMaster({
        items: i.data || [], warehouses: w.data || [], vendors: v.data || [], branches: b.data || [],
      }))
      .catch(() => { /* the panels show their own load errors */ })
      .finally(() => setLoading(false));
  }, []);

  const selectTab = (key) => setParams(key === TABS[0][0] ? {} : { tab: key }, { replace: true });
  const countFor = (key) => (n) => setCounts((c) => (c[key] === n ? c : { ...c, [key]: n }));

  if (loading) {
    return (
      <>
        <div className="page-header"><h1>Template</h1></div>
        <div className="card" style={{ padding: '2rem', color: 'var(--ink-3)' }}>Memuat…</div>
      </>
    );
  }

  return (
    <>
      <div className="page-header">
        <h1>Template</h1>
      </div>

      <div className="tpl-tabs" role="tablist" aria-label="Jenis template">
        {TABS.map(([key, label]) => (
          <button
            key={key}
            type="button"
            role="tab"
            id={`tpl-tab-${key}`}
            aria-selected={tab === key}
            aria-controls={`tpl-panel-${key}`}
            className={`tpl-tab${tab === key ? ' on' : ''}`}
            onClick={() => selectTab(key)}
          >
            {label}
            {counts[key] !== undefined && <span className="tpl-tab-count">{counts[key]}</span>}
          </button>
        ))}
      </div>

      {/* All three stay mounted: switching tabs to check how another template is
          set up should not throw away a half-filled form, and the counts in the
          tab strip are only honest if every list has actually been loaded. */}
      {TABS.map(([key]) => (
        <div
          key={key}
          role="tabpanel"
          id={`tpl-panel-${key}`}
          aria-labelledby={`tpl-tab-${key}`}
          hidden={tab !== key}
        >
          {key === 'pembelanjaan' && <DailyPurchasePanel master={master} onCount={countFor(key)} />}
          {key === 'invoice' && <InvoicePanel master={master} onCount={countFor(key)} />}
          {key === 'pengiriman' && <DispatchPanel master={master} onCount={countFor(key)} />}
        </div>
      ))}
    </>
  );
}
