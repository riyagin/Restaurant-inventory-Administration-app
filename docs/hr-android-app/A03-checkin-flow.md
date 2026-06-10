# Prompt A03 — Check-in/Check-out Flow (Live Recognition)

> Read `docs/hr-android-app/A00-overview.md` first. Requires A02 completed.

## Goal

The core kiosk screen: continuous camera preview that detects a face, matches it against enrolled employees, confirms identity, and submits the attendance event with the captured photo.

## Flow

```
Idle (preview + clock) → face detected (stable for ~1s)
  → crop/align → embed → cosine match against all enrolled employees
  → if best ≥ threshold AND margin to 2nd-best ≥ 0.05:
        Confirm card: photo, name, code, big buttons "Ya, Saya <Nama>" / "Bukan Saya"
        (auto-confirm after 3s countdown — configurable)
  → submit POST /device/event (event_type=auto, timestamp=now, photo=matched JPEG frame)
  → success screen 4s: "Selamat pagi, <Nama>! Masuk 07:58" (use server-resolved state:
     check_in → greeting by time of day; check_out → "Sampai jumpa, ...")
  → back to Idle
  → if no match: "Wajah tidak dikenali" + hint to contact admin; never guess.
```

## Requirements

1. **Performance**: analysis pipeline on a background executor with frame throttling (process every Nth frame); UI stays 60fps. Embedding+match budget < 200ms on a mid-range device.
2. **Basic liveness/anti-spoof** (pragmatic, not bulletproof): require a blink OR small head movement between two consecutive matched frames before confirming (ML Kit eye-open/euler angles). Configurable on/off (A05 settings). Document limitations honestly in README.
3. **Duplicate guard**: same employee within 5 minutes → friendly "Sudah tercatat" screen, no API call (server also dedups; this is UX).
4. **Photo evidence**: send the matched frame as compressed JPEG (~max 200KB, longest edge 720px).
5. **Time**: send device time RFC3339 with timezone; show a prominent clock on Idle. If device clock skew vs server (compare Date response header) > 2 min, show a persistent warning banner.
6. **Errors**: network failure → enqueue to the pending-events table (full offline handling arrives in A04 — for now insert + show "Tersimpan, akan dikirim ulang"). API 4xx → show message, do not enqueue.
7. **Accessibility/kiosk UX**: large text, high contrast, sound cue on success/failure (subtle), works in portrait & landscape.

## Definition of Done

Build/test/lint green. Unit tests: matcher (threshold + margin logic), duplicate guard, time formatting. Manual test script in README: enroll 2 people, check in, check out, unknown face, duplicate within 5 min.
