-- dev-seed-kasbon-leave.sql
-- Local-testing seed: exercises payroll's DEDUCTION side for the June 2026 period.
--   - 3 processed kasbons (cash advances) with a pending installment due in June,
--     so payroll deducts them from net pay (one has a 2nd installment due in July
--     that June payroll should NOT touch).
--   - 2 approved unpaid-leave (Izin Tanpa Gaji) requests inside June, so payroll
--     deducts daily_rate × working_days. The overlapping attendance rows are
--     flipped to status='leave' so the dashboard stays consistent.
--
-- Kasbon processing also posts to the Chart of Accounts (fund source debited,
-- "Piutang Karyawan" credited), mirroring service.ProcessKasbon.
--
-- Safe to re-run: rows are tagged with a marker in details/reason and skipped if
-- they already exist for that employee.
--
-- Run:  psql -U postgres -d inventory_app -f server-go/dev-seed-kasbon-leave.sql

DO $$
DECLARE
  v_fund        uuid;
  v_fund_name   text;
  v_admin       uuid;
  v_unpaid_type uuid;
  v_emp         uuid;
  v_kasbon      uuid;
  v_seq         int;
  v_num         text;
  v_res_month   date;
  i             int;

  k_codes  text[]   := ARRAY['EMP102','EMP105','EMP108'];
  k_total  bigint[] := ARRAY[1500000, 1000000, 2000000];
  k_june   bigint[] := ARRAY[ 750000, 1000000, 1000000];
  k_july   bigint[] := ARRAY[ 750000,       0, 1000000];  -- 0 = single installment

  l_codes  text[]   := ARRAY['EMP103','EMP110'];
  l_start  date[]   := ARRAY[DATE '2026-06-15', DATE '2026-06-22'];
  l_end    date[]   := ARRAY[DATE '2026-06-16', DATE '2026-06-22'];
  l_days   int[]    := ARRAY[2, 1];

  KMARK CONSTANT text := 'Kasbon dummy (seed test)';
  LMARK CONSTANT text := 'Cuti dummy (seed test)';
  n_kasbon int := 0;
  n_leave  int := 0;
BEGIN
  -- Fund source: lowest-numbered asset account that isn't Piutang Karyawan (10300).
  SELECT id, name INTO v_fund, v_fund_name
  FROM accounts
  WHERE account_type = 'asset' AND COALESCE(account_number, 0) <> 10300
  ORDER BY account_number NULLS LAST
  LIMIT 1;
  IF v_fund IS NULL THEN
    SELECT id, name INTO v_fund, v_fund_name FROM accounts ORDER BY account_number NULLS LAST LIMIT 1;
  END IF;
  IF v_fund IS NULL THEN
    RAISE EXCEPTION 'No accounts found to use as kasbon fund source.';
  END IF;

  SELECT id INTO v_admin FROM users WHERE role IN ('admin', 'manager') ORDER BY role LIMIT 1;

  SELECT id INTO v_unpaid_type FROM leave_types WHERE name = 'Izin Tanpa Gaji' LIMIT 1;
  IF v_unpaid_type IS NULL THEN
    RAISE EXCEPTION 'Leave type "Izin Tanpa Gaji" not found (migration 010 not applied?).';
  END IF;

  -- Next kasbon sequence for 2026.
  SELECT COALESCE(MAX((split_part(kasbon_number, '-', 3))::int), 0)
  INTO v_seq
  FROM kasbons WHERE kasbon_number LIKE 'KSB-2026-%';

  ---------------------------------------------------------------------------
  -- Kasbons
  ---------------------------------------------------------------------------
  FOR i IN 1 .. array_length(k_codes, 1) LOOP
    SELECT id INTO v_emp FROM employees WHERE employee_code = k_codes[i];
    IF v_emp IS NULL THEN CONTINUE; END IF;
    IF EXISTS (SELECT 1 FROM kasbons WHERE employee_id = v_emp AND details = KMARK) THEN
      CONTINUE;  -- already seeded
    END IF;

    v_seq := v_seq + 1;
    v_num := format('KSB-2026-%04s', lpad(v_seq::text, 4, '0'));
    v_res_month := CASE WHEN k_july[i] > 0 THEN DATE '2026-07-01' ELSE DATE '2026-06-01' END;

    INSERT INTO kasbons (
      kasbon_number, employee_id, amount, details, sending_method,
      fund_source_account_id, request_date, resolution_month, status,
      approved_by, approved_at, approval_note, processed_by, processed_at, created_by
    ) VALUES (
      v_num, v_emp, k_total[i], KMARK, 'transfer',
      v_fund, DATE '2026-05-15', v_res_month, 'processed',
      v_admin, TIMESTAMPTZ '2026-05-16 10:00+07', 'Disetujui (seed)', v_admin, TIMESTAMPTZ '2026-05-17 09:00+07', v_admin
    )
    RETURNING id INTO v_kasbon;

    -- June installment (due <= period month -> deducted by June payroll).
    INSERT INTO kasbon_installments (kasbon_id, due_month, amount, status)
    VALUES (v_kasbon, DATE '2026-06-01', k_june[i], 'pending');

    -- Optional July installment (due after June -> NOT deducted by June payroll).
    IF k_july[i] > 0 THEN
      INSERT INTO kasbon_installments (kasbon_id, due_month, amount, status)
      VALUES (v_kasbon, DATE '2026-07-01', k_july[i], 'pending');
    END IF;

    -- CoA posting mirroring service.ProcessKasbon: fund source debited by the full
    -- amount, Piutang Karyawan (10300) credited by the same.
    UPDATE accounts SET balance = balance - k_total[i] WHERE id = v_fund;
    UPDATE accounts SET balance = balance + k_total[i] WHERE account_number = 10300;

    n_kasbon := n_kasbon + 1;
  END LOOP;

  ---------------------------------------------------------------------------
  -- Approved unpaid leave
  ---------------------------------------------------------------------------
  FOR i IN 1 .. array_length(l_codes, 1) LOOP
    SELECT id INTO v_emp FROM employees WHERE employee_code = l_codes[i];
    IF v_emp IS NULL THEN CONTINUE; END IF;
    IF EXISTS (SELECT 1 FROM leave_requests WHERE employee_id = v_emp AND reason = LMARK) THEN
      CONTINUE;
    END IF;

    INSERT INTO leave_requests (
      employee_id, leave_type_id, start_date, end_date, day_count, reason,
      status, decided_by, decided_at, decision_note, created_by
    ) VALUES (
      v_emp, v_unpaid_type, l_start[i], l_end[i], l_days[i], LMARK,
      'approved', v_admin, now(), 'Disetujui (seed)', v_admin
    );

    -- Keep attendance consistent: mark those days as 'leave' (clear punches/flags).
    UPDATE attendance_records
    SET status = 'leave', check_in = NULL, check_out = NULL,
        check_in_source = NULL, check_out_source = NULL,
        is_late = false, late_minutes = 0, is_early_leave = false,
        early_leave_minutes = 0, is_missing_checkout = false
    WHERE employee_id = v_emp AND date BETWEEN l_start[i] AND l_end[i];

    n_leave := n_leave + 1;
  END LOOP;

  RAISE NOTICE 'Fund source account: %', v_fund_name;
  RAISE NOTICE 'Seeded % processed kasbon(s) and % approved unpaid-leave request(s).', n_kasbon, n_leave;
END $$;
