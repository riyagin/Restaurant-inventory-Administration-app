-- 041: Automatic payroll → ledger posting.
--
-- Closing a payroll period used to leave the ledger half-written: the close
-- itself debited each branch's expense account with gross pay and nothing else
-- (a one-sided entry), and an operator then had to press "Proses ke Akuntansi",
-- which posted a SECOND, different figure (net pay, Kas → "Beban Gaji - <cabang>")
-- from the browser, one HTTP call per branch. The expense was therefore counted
-- twice, and if nobody pressed the button it was counted once — one-sidedly.
--
-- Now the close queues exactly one balanced journal entry and a background worker
-- posts it. This table is the durable work item: it is written inside the close
-- transaction, so a crash between "closed" and "posted" leaves a pending row that
-- the startup sweep finds and retries. Without it, an async post that died with
-- the process would silently vanish.
--
-- One row per period (PK = period_id) makes double-posting impossible even if the
-- worker is somehow invoked twice; service.PostPayrollPeriod additionally checks
-- the journal for an existing 'payroll' entry before writing.
CREATE TABLE IF NOT EXISTS payroll_postings (
  period_id        UUID        PRIMARY KEY REFERENCES payroll_periods(id) ON DELETE CASCADE,
  -- pending → queued or mid-flight; posted → journal entry written; failed →
  -- every attempt so far errored, retried by the sweep until max attempts.
  status           TEXT        NOT NULL DEFAULT 'pending'
                               CHECK (status IN ('pending', 'posted', 'failed')),
  attempts         INT         NOT NULL DEFAULT 0,
  last_error       TEXT        NOT NULL DEFAULT '',
  journal_entry_id UUID        REFERENCES journal_entries(id) ON DELETE SET NULL,
  queued_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
  posted_at        TIMESTAMPTZ
);

-- The sweep's working set: everything not yet posted, oldest first.
CREATE INDEX IF NOT EXISTS payroll_postings_unposted_idx
  ON payroll_postings (queued_at)
  WHERE status <> 'posted';
