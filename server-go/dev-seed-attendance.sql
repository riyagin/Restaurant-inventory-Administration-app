-- dev-seed-attendance.sql
-- Local-testing seed: daily attendance for the PREVIOUS full month for every
-- active employee in the (single) seeded branch, so a payroll period for that
-- month can be generated end-to-end.
--
-- What it creates per employee, for each Mon–Sat in the period:
--   - mostly 'present' with realistic check-in/out times (some late, some early
--     leave, a few missing check-outs, ~5% absent)
--   - one public holiday (1st of the month) with ~40% present-on-holiday, which
--     feeds payroll's public_holiday_days -> holiday pay
--   - Sundays: no record (not a work day)
-- Anomaly flags (is_late / late_minutes / is_early_leave / is_missing_checkout)
-- are computed to match service.ComputeAnomalies against an 08:00–17:00 schedule
-- (grace 15 min, early-leave 30 min).
--
-- Also ensures the branch has a work_schedules row (schema-008 shape).
--
-- Safe to re-run: an (employee, date) that already has a row is skipped.
--
-- Adjust v_start / v_end below to target a different month.
--
-- Run:  psql -U postgres -d inventory_app -f server-go/dev-seed-attendance.sql

DO $$
DECLARE
  v_branch_id  uuid;
  v_branch_nm  text;
  v_start      date := DATE '2026-06-01';   -- previous full month (edit to retarget)
  v_end        date := DATE '2026-06-30';
  v_holiday    date := DATE '2026-06-01';   -- Hari Lahir Pancasila (present-on-holiday test)
  v_emp        record;
  v_day        date;
  v_isodow     int;
  v_is_holiday boolean;
  v_status     text;
  v_ci_min     int;
  v_co_min     int;
  v_is_late    boolean;
  v_late       int;
  v_is_early   boolean;
  v_early      int;
  v_missing    boolean;
  v_ci         timestamptz;
  v_co         timestamptz;
  r            double precision;
  n_rows       int := 0;
  n_present    int := 0;
  n_absent     int := 0;
  n_holiday    int := 0;
  n_emp        int := 0;
BEGIN
  SELECT id, name INTO v_branch_id, v_branch_nm FROM branches ORDER BY name LIMIT 1;
  IF v_branch_id IS NULL THEN
    RAISE EXCEPTION 'No branch found — seed a branch and employees first.';
  END IF;

  -- Ensure the branch work schedule exists (08:00–17:00, Mon–Sat).
  INSERT INTO work_schedules (branch_id, work_start, work_end, grace_minutes, early_leave_minutes, work_days)
  VALUES (v_branch_id, '08:00', '17:00', 15, 30, '{1,2,3,4,5,6}')
  ON CONFLICT (branch_id) DO UPDATE
    SET work_start = EXCLUDED.work_start,
        work_end = EXCLUDED.work_end,
        grace_minutes = EXCLUDED.grace_minutes,
        early_leave_minutes = EXCLUDED.early_leave_minutes,
        work_days = EXCLUDED.work_days;

  -- Ensure a public holiday inside the period so holiday pay can be tested.
  INSERT INTO public_holidays (date, name)
  VALUES (v_holiday, 'Hari Lahir Pancasila')
  ON CONFLICT (date) DO NOTHING;

  FOR v_emp IN
    SELECT id, employee_code FROM employees
    WHERE branch_id = v_branch_id AND status = 'active'
    ORDER BY employee_code
  LOOP
    n_emp := n_emp + 1;
    v_day := v_start;

    WHILE v_day <= v_end LOOP
      v_isodow := EXTRACT(ISODOW FROM v_day);   -- 1=Mon .. 7=Sun

      -- Sunday: not a work day, no record.
      IF v_isodow = 7 THEN
        v_day := v_day + 1;
        CONTINUE;
      END IF;

      -- Idempotency: never overwrite an existing record.
      IF EXISTS (SELECT 1 FROM attendance_records WHERE employee_id = v_emp.id AND date = v_day) THEN
        v_day := v_day + 1;
        CONTINUE;
      END IF;

      v_is_holiday := (v_day = v_holiday);

      -- Reset per-day state.
      v_is_late := false; v_late := 0;
      v_is_early := false; v_early := 0;
      v_missing := false;
      v_ci_min := NULL; v_co_min := NULL;
      v_ci := NULL; v_co := NULL;

      IF v_is_holiday THEN
        IF random() < 0.4 THEN
          v_status := 'present';                       -- worked the holiday
          v_ci_min := 480 + floor(random() * 20)::int; -- 08:00–08:20
          v_co_min := 1000 + floor(random() * 25)::int;-- ~16:40–17:05
        ELSE
          v_status := 'holiday';                       -- day off
        END IF;
      ELSE
        r := random();
        IF r < 0.05 THEN
          v_status := 'absent';                        -- ~5% no-show
        ELSE
          v_status := 'present';
          v_ci_min := 470 + floor(random() * 55)::int; -- 07:50–08:45 (some late)
          v_co_min := 980 + floor(random() * 130)::int;-- 16:20–18:30 (early leave / overtime)
          IF random() < 0.05 THEN
            v_co_min := NULL;                          -- occasional forgotten check-out
          END IF;
        END IF;
      END IF;

      -- Anomaly flags (present only), mirroring service.ComputeAnomalies.
      IF v_status = 'present' THEN
        IF v_ci_min IS NOT NULL AND v_ci_min > 495 THEN   -- work_start(480) + grace(15)
          v_is_late := true;
          v_late := v_ci_min - 480;
        END IF;
        IF v_co_min IS NOT NULL THEN
          IF v_co_min < 990 THEN                          -- work_end(1020) - early_leave(30)
            v_is_early := true;
            v_early := 1020 - v_co_min;
          END IF;
        ELSE
          v_missing := true;                              -- has check-in, no check-out, day is over
        END IF;
      END IF;

      -- Build timestamptz from Jakarta wall-clock minutes.
      IF v_ci_min IS NOT NULL THEN
        v_ci := (v_day + make_interval(mins => v_ci_min)) AT TIME ZONE 'Asia/Jakarta';
      END IF;
      IF v_co_min IS NOT NULL THEN
        v_co := (v_day + make_interval(mins => v_co_min)) AT TIME ZONE 'Asia/Jakarta';
      END IF;

      INSERT INTO attendance_records (
        employee_id, date, check_in, check_out, check_in_source, check_out_source,
        status, is_late, late_minutes, is_early_leave, early_leave_minutes, is_missing_checkout
      ) VALUES (
        v_emp.id, v_day, v_ci, v_co,
        CASE WHEN v_ci IS NOT NULL THEN 'face' END,
        CASE WHEN v_co IS NOT NULL THEN 'face' END,
        v_status, v_is_late, v_late, v_is_early, v_early, v_missing
      );

      n_rows := n_rows + 1;
      IF v_status = 'present' THEN
        n_present := n_present + 1;
      ELSIF v_status = 'absent' THEN
        n_absent := n_absent + 1;
      ELSE
        n_holiday := n_holiday + 1;
      END IF;

      v_day := v_day + 1;
    END LOOP;
  END LOOP;

  RAISE NOTICE 'Branch: % (%)', v_branch_nm, v_branch_id;
  RAISE NOTICE 'Attendance seeded for % (% employees): % rows total — present %, absent %, holiday-off %.',
    to_char(v_start, 'Mon YYYY'), n_emp, n_rows, n_present, n_absent, n_holiday;
END $$;
