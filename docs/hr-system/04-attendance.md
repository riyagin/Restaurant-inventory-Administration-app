# Prompt 04 — Attendance System (Face API + Fingerprint Import + Dashboard)

> Read `docs/hr-system/00-overview.md` first. Requires prompt 01 completed. The Android app that produces face check-ins is a separate project (`docs/hr-android-app/`) — **this prompt builds the API it talks to**.

## Goal

Attendance records with two sources: **facial recognition** (primary, pushed by the Android app) and **fingerprint scanner export** (backup, imported by admin from a file). Dashboard shows per-employee attendance with source indicator, absent flag, and anomaly flags (late beyond grace period / left much earlier).

## Database

Migration `hr_attendance`:

```sql
work_schedules (                -- per-branch schedule & thresholds
  id UUID PK,
  branch_id UUID NOT NULL UNIQUE REFERENCES branches(id),
  work_start TIME NOT NULL DEFAULT '08:00',
  work_end TIME NOT NULL DEFAULT '17:00',
  grace_minutes INT NOT NULL DEFAULT 15,         -- late beyond this = anomaly
  early_leave_minutes INT NOT NULL DEFAULT 30,   -- leaving earlier than work_end - this = anomaly
  work_days INT[] NOT NULL DEFAULT '{1,2,3,4,5,6}'  -- ISO weekday numbers
)

public_holidays ( id UUID PK, date DATE NOT NULL UNIQUE, name TEXT NOT NULL )

attendance_devices (            -- Android devices / scanners allowed to push
  id UUID PK, name TEXT NOT NULL, branch_id UUID REFERENCES branches(id),
  api_key_hash TEXT NOT NULL, is_active BOOLEAN NOT NULL DEFAULT true,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
)

attendance_records (
  id UUID PK,
  employee_id UUID NOT NULL REFERENCES employees(id),
  date DATE NOT NULL,
  check_in TIMESTAMPTZ,
  check_out TIMESTAMPTZ,
  check_in_source TEXT CHECK (check_in_source IN ('face','fingerprint','manual')),
  check_out_source TEXT CHECK (check_out_source IN ('face','fingerprint','manual')),
  check_in_photo_path TEXT,                -- face check-in evidence
  device_id UUID REFERENCES attendance_devices(id),
  status TEXT NOT NULL DEFAULT 'present'
    CHECK (status IN ('present','absent','leave','holiday')),
  is_late BOOLEAN NOT NULL DEFAULT false,
  late_minutes INT NOT NULL DEFAULT 0,
  is_early_leave BOOLEAN NOT NULL DEFAULT false,
  early_leave_minutes INT NOT NULL DEFAULT 0,
  is_missing_checkout BOOLEAN NOT NULL DEFAULT false,
  note TEXT,
  UNIQUE (employee_id, date)
)

fingerprint_imports (           -- import batch header
  id UUID PK, filename TEXT, imported_by UUID REFERENCES users(id),
  row_count INT, matched_count INT, created_at TIMESTAMPTZ NOT NULL DEFAULT now()
)
```

## Merge & Anomaly Rules

1. **Face is primary**: a face event always wins; fingerprint data only fills `check_in`/`check_out` when that field is empty or only has a fingerprint source. Never overwrite a `face` value with `fingerprint`.
2. First event of the day = check_in, last = check_out (an event within 5 min of an existing one is deduplicated).
3. Anomaly computation (service `internal/service/attendance.go`, re-runs whenever a record changes), using the employee's branch `work_schedules`:
   - `is_late` = check_in > work_start + grace_minutes (store late_minutes past work_start).
   - `is_early_leave` = check_out < work_end − early_leave_minutes.
   - `is_missing_checkout` = has check_in, no check_out, day is over.
   - **Absent** = scheduled work day, not a holiday, no leave (status `leave` set by prompt 06), no record → a nightly reconciliation job (goroutine + `time.Ticker`, like the token-blocklist cleaner) inserts `status='absent'` rows for the previous day. Also expose `POST /api/hr/attendance/reconcile?date=` for manual/backfill runs.

## Endpoints

**Device API** (no JWT — authenticated by `X-Device-Key` checked against `attendance_devices.api_key_hash`, rate-limited):
- `POST /api/hr/attendance/device/event` — body: `employee_code`, `event_type` (`check_in|check_out|auto`), `timestamp` (RFC3339), multipart photo optional. `auto` = server decides in/out by existing record. Responds with the resolved record state (so the app can show "Selamat pagi, masuk 07:58").
- `GET /api/hr/attendance/device/employees` — roster for the device's branch: `employee_code`, `full_name`, `photo_path` (the app syncs this for face enrollment).

**Admin API** (JWT):
- `GET /api/hr/attendance` — filters: date range, branch, employee, status, source, anomaly-only; returns records with employee name/code.
- `PUT /api/hr/attendance/:id` — manual correction (source becomes `manual`, require note, logActivity).
- `POST /api/hr/attendance/fingerprint-import/parse` and `/confirm` — two-phase import like prompt 03. **File format is pending from the user** — build a `FingerprintParser` interface in `service/` with one placeholder implementation: generic CSV `employee_code,timestamp` (configurable column order documented in code). Mark with a clear TODO so a later session can drop in the real device format without touching the import flow. Unmatched employee codes are reported, not fatal.
- CRUD `work_schedules` (per branch), `public_holidays`, `attendance_devices` (create returns the API key **once**, store only hash).

## Frontend

1. **AttendanceDashboard** (`/hr/attendance`) — default view: selected date, all active employees as rows: name, branch, check-in/out times, **source badge per event** (ikon wajah = "Wajah", ikon sidik jari = "Sidik Jari", "Manual"), status chip (Hadir / Absen / Cuti / Libur), anomaly chips ("Terlambat 23 mnt", "Pulang Awal", "Tidak Absen Pulang"). Filters: branch, status, anomaly-only toggle, search. Month view per employee available from EmployeeDetail "Absensi" tab (fill the prompt-01 stub): calendar/table of the month with the same indicators.
2. **FingerprintImport** (`/hr/attendance/import`, admin/manager) — upload → preview (matched/unmatched rows) → confirm; note in UI that face data is never overwritten.
3. **AttendanceSettings** (`/hr/attendance/settings`, admin/manager) — work schedules per branch, public holidays list, device management (register device → show API key once).

## Definition of Done

Standard checklist + service tests: merge precedence (face beats fingerprint), dedup window, late/early-leave math across grace boundaries, absent reconciliation skips holidays/non-work-days.
