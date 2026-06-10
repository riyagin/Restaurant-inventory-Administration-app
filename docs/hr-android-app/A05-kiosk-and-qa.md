# Prompt A05 — Kiosk Mode, Settings & Final QA

> Read `docs/hr-android-app/A00-overview.md` first. Requires A04 completed. Final session for the app.

## Goal

Lock the device down as a dedicated attendance kiosk, finish the admin settings area, and run a full QA pass.

## Tasks

1. **Kiosk mode**:
   - App auto-launches the check-in screen (A03) on boot (`BOOT_COMPLETED` + home-screen/launcher intent category so it can act as the device launcher when chosen).
   - Use Android **screen pinning / lock task mode** where available; document Device Owner provisioning (`adb shell dpm set-device-owner ...`) in README as the recommended deployment for full lock-down.
   - Keep screen awake while charging (`FLAG_KEEP_SCREEN_ON` + charging detection); optional dimmed idle state after N minutes showing only the clock, waking on face detection.
2. **Admin area (PIN-gated)**: long-press 3s on the clock → PIN prompt (`kiosk_pin`, set during onboarding or here):
   - Settings: server URL, device name, match threshold slider (with live test), liveness on/off, auto-confirm seconds, duplicate-guard minutes, photo retention days.
   - Enrollment screen (A02), Sync status (A04), roster re-sync, app version + device key fingerprint (last 4 chars only).
3. **Hardening**: obfuscation off for now but strip logs in release; certificate pinning optional flag; deny screenshots on enrollment/admin screens (`FLAG_SECURE`); crash handler that restarts the kiosk activity.
4. **Release build**: signed release config docs, versionName 1.0.0, ProGuard rules for TFLite/Retrofit/Room verified.
5. **Full QA pass** — execute and check off in `QA-CHECKLIST.md`:
   - Onboarding on a wiped device → enrollment of 3 employees → a day of check-in/out events visible on the web AttendanceDashboard with source "Wajah" and photo evidence.
   - Unknown face rejected; photo-of-a-photo rejected when liveness is on (note the limitation when off).
   - Offline day: 5 events queued, synced correctly with original timestamps.
   - Reboot → app returns to kiosk screen without interaction.
   - Threshold slider: lowering it demonstrably loosens matching (documented test).
   - Backend integration gaps recorded in `API-GAPS.md` reviewed and either resolved or handed off.

## Definition of Done

`./gradlew assembleRelease test lint` green; QA checklist committed and complete; README covers provisioning, enrollment SOP for branch managers (Indonesian), and troubleshooting.
