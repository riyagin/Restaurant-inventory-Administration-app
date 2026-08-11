-- 066: Record *when* an invoice was settled, separately from when it was entered.
--
-- Settlement was posted with `time.Now()` as its journal date, so an invoice paid
-- last Friday and keyed in on Monday landed in Monday's books. Payment is a cash
-- event with its own date, and the person recording it usually knows that date —
-- so it becomes an input, both on the journal entry and here on the invoice.
--
-- `payment_date` holds the date of the *latest* settlement, not a history: the
-- per-payment record already exists as one `journal_entries` row per payment
-- (source_type = 'invoice_payment'), which is where a partial invoice's earlier
-- instalments stay visible. This column exists so the invoice list and detail can
-- answer "when was this paid" without a join.

ALTER TABLE invoices ADD COLUMN IF NOT EXISTS payment_date DATE;

-- Backfill from what actually posted. The settlement entry's date is the truth
-- for anything already paid; invoices settled at creation time posted on the
-- invoice date, so the two agree there. Anything with no settlement entry (older
-- rows predating the journal) falls back to the invoice date rather than staying
-- blank — a blank would read as "not paid yet" on a paid invoice.
UPDATE invoices i
SET payment_date = COALESCE(
    (SELECT MAX(je.entry_date) FROM journal_entries je
     WHERE je.source_type = 'invoice_payment' AND je.source_id = i.id),
    i.date
)
WHERE i.payment_status IN ('paid', 'partial') AND i.payment_date IS NULL;
