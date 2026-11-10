package service

import "time"

// Business time zone.
//
// Timestamps are stored as timestamptz (absolute instants), so the database's
// own TimeZone setting decides only how psql prints them, never what is stored.
// Everything that needs a WALL CLOCK — "was this check-in late?", "which day does
// this punch belong to?", "what is today?" — must be read in WIB, the zone the
// branches and the attendance devices run in, never in the server process's
// local zone (a VPS is usually UTC, a dev laptop is WIB, and the answers must not
// depend on which one runs the code).
//
// Indonesia observes no daylight saving, so a fixed +07:00 offset is exact and
// needs no tzdata on the host.
var WIB = time.FixedZone("WIB", 7*60*60)

// InWIB returns t on the WIB wall clock.
func InWIB(t time.Time) time.Time { return t.In(WIB) }

// WIBDate returns the WIB calendar date of instant t, as a UTC-midnight
// time.Time — the representation the code uses for `date` columns.
func WIBDate(t time.Time) time.Time {
	w := t.In(WIB)
	return time.Date(w.Year(), w.Month(), w.Day(), 0, 0, 0, 0, time.UTC)
}

// TodayWIB is the current WIB calendar date (UTC-midnight representation).
func TodayWIB() time.Time { return WIBDate(time.Now()) }

// WIBWallClock interprets a calendar date (any location; only Y/M/D are used)
// plus minutes since midnight as a WIB instant.
func WIBWallClock(date time.Time, minutes int) time.Time {
	return time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, WIB).Add(time.Duration(minutes) * time.Minute)
}
