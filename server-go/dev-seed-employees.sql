-- dev-seed-employees.sql
-- Local-testing seed: 20 active employees in ONE branch, each with a complete
-- (open) wage structure and attached wage components.
--
-- Safe to re-run: employees are keyed by employee_code (EMP101..EMP120) and
-- skipped if they already exist, so it will not duplicate rows.
--
-- Run:  psql -U postgres -d inventory_app -f server-go/dev-seed-employees.sql
--
-- Amounts are whole rupiah (matching cmd/seed-hr and the HR wage queries),
-- NOT cents.

DO $$
DECLARE
  v_branch_id    uuid;
  v_branch_name  text;
  v_makan_id     uuid;
  v_transport_id uuid;
  v_bpjs_id      uuid;
  v_pos_ids      uuid[];
  v_emp_id       uuid;
  v_struct_id    uuid;
  v_base         bigint;
  v_daily        bigint;
  v_code         text;
  v_name         text;
  v_join         date;
  v_dob          date;
  v_pos_id       uuid;
  v_bank         text;
  n_created      int := 0;
  i              int;

  v_names text[] := ARRAY[
    'Budi Hartono','Dewi Lestari','Agus Salim','Rina Wijaya','Eko Prasetyo',
    'Sri Mulyani','Joko Purnomo','Ani Setiawati','Bambang Pamungkas','Citra Kirana',
    'Dodi Sudrajat','Endah Puspita','Fajar Nugroho','Gita Savitri','Hendra Gunawan',
    'Indah Permata','Krisna Mukti','Lia Amelia','Maman Suherman','Nia Andini'
  ];
  v_positions text[] := ARRAY['Kasir','Koki','Barista','Pelayan','Supervisor'];
  v_banks     text[] := ARRAY['BCA','BNI','BRI','Mandiri'];
  v_pos_name  text;
BEGIN
  ---------------------------------------------------------------------------
  -- 1. Pick a single branch (the first, alphabetically).
  ---------------------------------------------------------------------------
  SELECT id, name INTO v_branch_id, v_branch_name
  FROM branches ORDER BY name LIMIT 1;

  IF v_branch_id IS NULL THEN
    RAISE EXCEPTION 'No branch found — create a branch before seeding employees.';
  END IF;
  RAISE NOTICE 'Seeding into branch: % (%)', v_branch_name, v_branch_id;

  ---------------------------------------------------------------------------
  -- 2. Ensure positions exist, then collect their ids for round-robin assignment.
  ---------------------------------------------------------------------------
  FOREACH v_pos_name IN ARRAY v_positions LOOP
    INSERT INTO positions (name, is_active) VALUES (v_pos_name, true)
    ON CONFLICT (name) DO NOTHING;
  END LOOP;
  SELECT array_agg(id ORDER BY name) INTO v_pos_ids
  FROM positions WHERE name = ANY(v_positions);

  ---------------------------------------------------------------------------
  -- 3. Ensure wage components exist (2 allowances + 1 deduction).
  ---------------------------------------------------------------------------
  INSERT INTO wage_components (name, type, is_fixed, is_active)
    VALUES ('Tunjangan Makan', 'allowance', true, true)
    ON CONFLICT (name) DO NOTHING;
  INSERT INTO wage_components (name, type, is_fixed, is_active)
    VALUES ('Tunjangan Transport', 'allowance', true, true)
    ON CONFLICT (name) DO NOTHING;
  INSERT INTO wage_components (name, type, is_fixed, is_active)
    VALUES ('Potongan BPJS', 'deduction', true, true)
    ON CONFLICT (name) DO NOTHING;

  SELECT id INTO v_makan_id     FROM wage_components WHERE name = 'Tunjangan Makan';
  SELECT id INTO v_transport_id FROM wage_components WHERE name = 'Tunjangan Transport';
  SELECT id INTO v_bpjs_id      FROM wage_components WHERE name = 'Potongan BPJS';

  ---------------------------------------------------------------------------
  -- 4. Create 20 employees + wage structures + components.
  ---------------------------------------------------------------------------
  FOR i IN 1..20 LOOP
    v_code := 'EMP' || lpad((100 + i)::text, 3, '0');   -- EMP101 .. EMP120
    v_name := v_names[i];
    v_join := (CURRENT_DATE - (i * 30) * INTERVAL '1 day')::date;  -- spread ~20 months
    v_dob  := (DATE '1990-01-01' + (i * 97) * INTERVAL '1 day')::date;
    v_pos_id := v_pos_ids[((i - 1) % array_length(v_pos_ids, 1)) + 1];
    v_bank := v_banks[((i - 1) % array_length(v_banks, 1)) + 1];

    INSERT INTO employees (
      employee_code, full_name, dob, join_date, position_id, branch_id,
      phone, email, address, national_id,
      bank_name, bank_account_number, bank_account_holder, status
    ) VALUES (
      v_code, v_name, v_dob, v_join, v_pos_id, v_branch_id,
      '08' || lpad((100000000 + i * 111111)::text, 10, '0'),
      lower(replace(v_name, ' ', '.')) || '@example.com',
      'Jl. Contoh No. ' || i || ', Kota Testing',
      '327' || lpad((i * 1234567 % 10000000000)::text, 13, '0'),
      v_bank, lpad((1000000000 + i * 7777)::text, 10, '0'), v_name, 'active'
    )
    ON CONFLICT (employee_code) DO NOTHING
    RETURNING id INTO v_emp_id;

    IF v_emp_id IS NULL THEN
      CONTINUE;  -- already existed; leave its data untouched
    END IF;

    -- Base salary Rp 3.000.000 .. Rp 4.750.000, daily = base / 26.
    v_base  := 3000000 + ((i - 1) % 8) * 250000;
    v_daily := v_base / 26;

    INSERT INTO wage_structures (
      id, employee_id, base_salary, working_days_per_month, daily_rate,
      effective_date, end_date, created_by
    ) VALUES (
      gen_random_uuid(), v_emp_id, v_base, 26, v_daily, v_join, NULL, NULL
    )
    RETURNING id INTO v_struct_id;

    INSERT INTO employee_wage_components (id, wage_structure_id, wage_component_id, amount)
      VALUES (gen_random_uuid(), v_struct_id, v_makan_id, 250000);
    INSERT INTO employee_wage_components (id, wage_structure_id, wage_component_id, amount)
      VALUES (gen_random_uuid(), v_struct_id, v_transport_id, 150000);
    INSERT INTO employee_wage_components (id, wage_structure_id, wage_component_id, amount)
      VALUES (gen_random_uuid(), v_struct_id, v_bpjs_id, 100000);

    n_created := n_created + 1;
  END LOOP;

  RAISE NOTICE 'Done. % new employees created (EMP101..EMP120).', n_created;
END $$;
