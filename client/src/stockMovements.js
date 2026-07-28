// Shared presentation metadata for `stock_history` rows.
//
// Keep the type keys in sync with the values the Go handlers write (see the
// `Type:` fields on service.StockHistoryParams). Legacy Node-era types are kept
// at the bottom so old rows still render with a proper label.

export const SOURCE_PATH = {
  invoice:     (id) => `/invoices/view/${id}`,
  transfer:    (id) => `/transfers/group/${id}`,
  dispatch:    (id) => `/dispatches/${id}`,
  opname:      (id) => `/stock-opname/detail/${id}`,
  enumeration: () => '/enumerations',
};

export const TYPE_LABEL = {
  purchase:             'Pembelian',
  invoice:              'Invoice',
  transfer:             'Transfer',
  transfer_edit:        'Koreksi Transfer',
  transfer_cancel:      'Batal Transfer',
  dispatch:             'Pengeluaran',
  dispatch_edit:        'Koreksi Pengeluaran',
  dispatch_cancel:      'Batal Pengeluaran',
  production:           'Produksi',
  opname:               'Stock Opname',
  enumeration:          'Enumerasi',
  enumeration_reversal: 'Batal Enumerasi',
  // legacy (Node backend)
  manual_in:            'Manual In',
  manual_out:           'Manual Out',
  manual_adjustment:    'Adjustment',
  pemakaian:            'Pemakaian',
  SO:                   'SO',
};

const BLUE   = { background: '#e8f0fe', color: '#4f8ef7' };
const GREEN  = { background: '#e6f9f0', color: '#27ae60' };
const RED    = { background: '#fdecea', color: '#e74c3c' };
const ORANGE = { background: '#fff3e0', color: '#f57c00' };
const YELLOW = { background: '#fef9e7', color: '#e67e22' };
const PURPLE = { background: '#f3e8ff', color: '#8b5cf6' };
const GREY   = { background: '#eee', color: '#555' };

export const TYPE_STYLE = {
  purchase:             GREEN,
  invoice:              BLUE,
  transfer:             BLUE,
  transfer_edit:        YELLOW,
  transfer_cancel:      GREY,
  dispatch:             PURPLE,
  dispatch_edit:        YELLOW,
  dispatch_cancel:      GREY,
  production:           ORANGE,
  opname:               ORANGE,
  enumeration:          PURPLE,
  enumeration_reversal: GREY,
  manual_in:            GREEN,
  manual_out:           RED,
  manual_adjustment:    YELLOW,
  pemakaian:            PURPLE,
  SO:                   ORANGE,
};

export const typeLabel = (t) => TYPE_LABEL[t] ?? t;
export const typeStyle = (t) => TYPE_STYLE[t] ?? GREY;
export const sourcePath = (sourceType, sourceId) =>
  sourceType && sourceId ? (SOURCE_PATH[sourceType]?.(sourceId) ?? null) : null;
