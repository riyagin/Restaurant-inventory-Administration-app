package service

import (
	"errors"
	"testing"
	"time"
)

func TestResolveDevicePunchTime(t *testing.T) {
	received := time.Date(2026, 11, 13, 1, 0, 0, 500_000_000, time.UTC) // 08:00:00.5 WIB
	recv := received.Truncate(time.Second)
	ms := func(d time.Duration) *int64 { v := d.Milliseconds(); return &v }
	at := func(d time.Duration) *time.Time { v := received.Add(d); return &v }

	cases := []struct {
		name    string
		age     *int64
		device  *time.Time
		want    time.Time
		source  string
		wantErr error
	}{
		// The case the change exists for: tablet clock set back two hours, live
		// check-in. The device clock is ignored entirely.
		{"live, spoofed clock ignored", ms(150 * time.Millisecond), at(-2 * time.Hour), recv, PunchTimeServer, nil},
		{"live, no device time", ms(0), nil, recv, PunchTimeServer, nil},
		// Offline for 40 minutes: anchored to the server, not the device clock.
		{"queued", ms(40 * time.Minute), at(+3 * time.Hour), recv.Add(-40 * time.Minute), PunchTimeQueued, nil},
		{"negative age", ms(-time.Millisecond), nil, time.Time{}, "", ErrPunchAgeInvalid},
		{"queued too old", ms(15 * 24 * time.Hour), nil, time.Time{}, "", ErrPunchTooOld},
		// No age (rebooted while offline, or a legacy app build).
		{"legacy, clock correct", nil, at(-30 * time.Second), recv, PunchTimeServer, nil},
		{"legacy, clock ahead", nil, at(+10 * time.Minute), time.Time{}, "", ErrPunchInFuture},
		{"rebooted offline event", nil, at(-3 * time.Hour), received.Add(-3 * time.Hour).Truncate(time.Second), PunchTimeDevice, nil},
		{"device time too old", nil, at(-20 * 24 * time.Hour), time.Time{}, "", ErrPunchTooOld},
		{"nothing sent", nil, nil, time.Time{}, "", ErrPunchTimeMissing},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ResolveDevicePunchTime(received, c.age, c.device)
			if c.wantErr != nil {
				if !errors.Is(err, c.wantErr) {
					t.Fatalf("err = %v, want %v", err, c.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !got.At.Equal(c.want) || got.Source != c.source {
				t.Fatalf("got %s/%s, want %s/%s", got.At, got.Source, c.want, c.source)
			}
			if (got.Source != PunchTimeDevice) != got.Verified() {
				t.Fatalf("Verified() inconsistent with source %s", got.Source)
			}
		})
	}
}
