package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"inventory-app/server-go/internal/db"
)

// Kiosk punch timing: the SERVER's clock decides when a face / manual punch
// happened, never the tablet's wall clock (which anyone at the kiosk can change).
//
// The app sends age_ms — how long ago the event was captured, measured on
// Android's monotonic clock (SystemClock.elapsedRealtime, time since boot, which
// changing the wall clock does not move). The punch is then
//
//	received_at − age_ms
//
// For a live check-in age_ms is a few milliseconds and that is simply the
// server's time. For an event captured offline and delivered later it is still
// anchored to the server clock: only the elapsed duration comes from the device.
//
// The monotonic clock restarts at boot, so an offline event whose tablet
// rebooted before it could be sent has no age_ms. That punch (and any event from
// an app build that predates age_ms) falls back to the device's own timestamp
// and is marked PunchTimeDevice — unverified — unless that timestamp is within
// DeviceClockTolerance of the server, in which case the server time is used.

const (
	PunchTimeServer = "server" // stamped by the server on receipt
	PunchTimeQueued = "queued" // server receipt minus the device's monotonic age
	PunchTimeDevice = "device" // the device's own clock; not verifiable
)

const (
	// An event this young is a live check-in, not a queued one.
	LiveEventMaxAge = 60 * time.Second
	// Queued events older than this are refused: a kiosk that has been offline
	// for two weeks needs a human to look at it, not a silent backfill.
	MaxQueuedEventAge = 14 * 24 * time.Hour
	// A device timestamp this close to the server is treated as "now".
	DeviceClockTolerance = 2 * time.Minute
	// A device timestamp this far ahead of the server is refused.
	DeviceFutureTolerance = 5 * time.Minute
)

var (
	ErrPunchAgeInvalid  = errors.New("age_ms tidak valid")
	ErrPunchTooOld      = errors.New("event absensi terlalu lama (lebih dari 14 hari), hubungi HR")
	ErrPunchInFuture    = errors.New("waktu perangkat berada di masa depan, periksa jam perangkat")
	ErrPunchTimeMissing = errors.New("timestamp atau age_ms wajib diisi")
)

// DevicePunchTime is the resolved time of a kiosk punch and where it came from.
type DevicePunchTime struct {
	At             time.Time
	Source         string     // PunchTimeServer | PunchTimeQueued | PunchTimeDevice
	DeviceReported *time.Time // what the device clock said, kept for audit
}

// Verified reports whether the time is anchored to the server clock.
func (t DevicePunchTime) Verified() bool { return t.Source != PunchTimeDevice }

// ResolveDevicePunchTime decides when a kiosk punch happened. received is the
// server time the request arrived; ageMs (nil when absent) is the device's
// monotonic age of the event; deviceTs (nil when absent) is the device wall clock.
func ResolveDevicePunchTime(received time.Time, ageMs *int64, deviceTs *time.Time) (DevicePunchTime, error) {
	exact := received
	received = received.Truncate(time.Second)

	if ageMs != nil {
		if *ageMs < 0 {
			return DevicePunchTime{}, ErrPunchAgeInvalid
		}
		age := time.Duration(*ageMs) * time.Millisecond
		if age > MaxQueuedEventAge {
			return DevicePunchTime{}, ErrPunchTooOld
		}
		src := PunchTimeServer
		if age > LiveEventMaxAge {
			src = PunchTimeQueued
		}
		// Subtract from the exact receive time, then truncate: truncating first
		// would shift a live punch a second early.
		return DevicePunchTime{At: exact.Add(-age).Truncate(time.Second), Source: src, DeviceReported: deviceTs}, nil
	}

	if deviceTs == nil {
		return DevicePunchTime{}, ErrPunchTimeMissing
	}
	diff := deviceTs.Sub(received)
	switch {
	case diff > DeviceFutureTolerance:
		return DevicePunchTime{}, ErrPunchInFuture
	case diff >= -DeviceClockTolerance && diff <= DeviceClockTolerance:
		return DevicePunchTime{At: received, Source: PunchTimeServer, DeviceReported: deviceTs}, nil
	case -diff > MaxQueuedEventAge:
		return DevicePunchTime{}, ErrPunchTooOld
	}
	return DevicePunchTime{At: deviceTs.Truncate(time.Second), Source: PunchTimeDevice, DeviceReported: deviceTs}, nil
}

// DevicePunchResult is what LogDevicePunch stored (or found already stored).
type DevicePunchResult struct {
	At        time.Time
	Source    string // time source of the stored punch
	WorkDate  time.Time
	Schedule  Schedule
	Duplicate bool // the client_event_id was already logged; nothing new was inserted
}

// ErrClientEventConflict means a client_event_id was reused for another employee.
var ErrClientEventConflict = errors.New("client_event_id sudah dipakai untuk karyawan lain")

// LogDevicePunch is LogPunch for kiosk events: it records the time source and
// the device's claimed time, and is idempotent on clientEventID — a retry of an
// event that was already logged returns the ORIGINAL punch instead of logging a
// second one at a slightly different time. tx must be the transaction q runs in.
func LogDevicePunch(ctx context.Context, q *db.Queries, tx db.DBTX, employeeID pgtype.UUID,
	p Punch, t DevicePunchTime, clientEventID pgtype.UUID, base Schedule, deviceID pgtype.UUID) (DevicePunchResult, error) {

	schedFor := func(d time.Time) Schedule { return ScheduleWithShiftQ(ctx, q, base, employeeID, d) }

	if clientEventID.Valid {
		var (
			empID    pgtype.UUID
			at       time.Time
			workDate pgtype.Date
			source   string
		)
		err := tx.QueryRow(ctx, `
			SELECT employee_id, punched_at, work_date, time_source
			FROM attendance_punches WHERE client_event_id = $1`, clientEventID).
			Scan(&empID, &at, &workDate, &source)
		switch {
		case err == nil:
			if empID.Bytes != employeeID.Bytes {
				return DevicePunchResult{}, ErrClientEventConflict
			}
			return DevicePunchResult{At: at, Source: source, WorkDate: workDate.Time,
				Schedule: schedFor(workDate.Time), Duplicate: true}, nil
		case !errors.Is(err, pgx.ErrNoRows):
			return DevicePunchResult{}, fmt.Errorf("lookup client event: %w", err)
		}
	}

	day, sched := PickWorkDate(t.At, schedFor)
	var reported pgtype.Timestamptz
	if t.DeviceReported != nil {
		reported = pgtype.Timestamptz{Time: *t.DeviceReported, Valid: true}
	}
	// No conflict target: covers both the (employee, punched_at, source) key and
	// the client_event_id index. A concurrent retry that loses the race inserts
	// nothing, and the day is rebuilt from the punch that won.
	_, err := tx.Exec(ctx, `
		INSERT INTO attendance_punches
		  (employee_id, punched_at, work_date, source, direction_hint, device_id,
		   time_source, device_reported_at, client_event_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT DO NOTHING`,
		employeeID, t.At, pgtype.Date{Time: day, Valid: true}, p.Source,
		pgtype.Text{String: p.Hint, Valid: p.Hint != ""}, deviceID,
		t.Source, reported, clientEventID)
	if err != nil {
		return DevicePunchResult{}, fmt.Errorf("insert punch: %w", err)
	}
	return DevicePunchResult{At: t.At, Source: t.Source, WorkDate: day, Schedule: sched}, nil
}
