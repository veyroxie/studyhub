# NFC Wristband Attendance — Plan

Status: **planning** (no code written yet). Hardware not yet purchased.
Decided 2026-06-17. **Hardware switched to M5Stack StickS3 + PN532 NFC Unit (2026-06-17).**

## Goal

Kids tap an NTAG213 silicone wristband on a reader at the centre; the system
records check-in / check-out against StudyHub's existing `attendance` table.
The reader's screen shows the kid's name + status for instant, clear feedback
(better for little ones; lets staff spot a kid tapping someone else's band).

## Hardware (per reader) — M5Stack build

Chosen over bare ESP32 + breadboard: integrated, no soldering, has screen +
speaker + battery in a finished case. Cousin's pick, verified compatible.

- **M5Stack StickS3** (~RM110) — ESP32-S3, 1.14" LCD, speaker (ES8311), MEMS mic,
  WiFi 2.4GHz, 250mAh battery, Grove port + GPIO header. This is the brain +
  screen + beep. **Has NO NFC reader on its own.**
- **M5Stack NFC Unit — PN532 version** (Grove). The actual reader. 13.56MHz,
  reads NTAG213, plugs into the StickS3 Grove port (no soldering).
  ⚠️ Must be the **PN532** unit, NOT the "RFID2 Unit" (WS1850S/MFRC522-family —
  flaky for NTAG, same reason we rejected RC522).
- **NTAG213 silicone wristbands** (bulk + spares). ⚠️ Must be **13.56MHz / NTAG213**,
  NOT 125kHz (EM4100/T5577) — the PN532 cannot read 125kHz tags. Only the 7-byte
  UID is used.
- Grove cable comes with the NFC unit. 5V USB-C wall adapter + data cable to power.

**No longer needed** (StickS3 replaces them): separate ESP32 DevKit, breadboard,
buzzer (built-in speaker), LEDs + resistors (screen shows status), jumper wires,
separate enclosure.

## Wiring

Plug-and-play: NFC Unit → StickS3 **Grove port** via the included Grove cable.
No GPIO wiring, no soldering. (The old bare-ESP32 GPIO21/22 + LED/buzzer diagram
is obsolete and removed.)

## Firmware (M5Stack)

- Programmed via Arduino IDE (M5Stack board package, board = StickS3 / ESP32-S3)
  or PlatformIO.
- Libraries: **M5Unified** (screen, speaker, buttons) + **M5Unit-NFC** (reads the
  PN532 unit over Grove I2C). Adafruit_PN532 also works over I2C if needed.
- Feedback via screen instead of LEDs:
  - check-in  → green screen "Welcome, <name>"  + short beep
  - check-out → green screen "Goodbye, <name>"   + short beep
  - unknown   → red screen "Not recognised"      + two beeps
  - screen can be blanked between taps (config) if it distracts kids
- 4 values to edit at top: WiFi SSID, WiFi password, server URL, device secret.

## Key design decisions (unchanged by hardware switch)

1. **Auto-match scheduled class** — tap attaches the `class_id` scheduled around
   tap time (window start−30min .. end+30min); falls back to `class_id NULL`
   building presence if no class in window. Ambiguity → start nearest tap time, log it.
2. **One reader toggles in/out** — 1st tap for (student, today, class_id) = check_in,
   2nd = check_out. Toggle is per matched class.
3. **Server-side debounce** ~5s per (uid, class_id) so a double-tap doesn't flip in→out.
4. **Device-token auth, public route** like iCal (`handlers_ical.go:77`) — NOT behind
   user-JWT cookie. `readers(device_id, tenant_id, secret)` table.

## Backend integration (StudyHub-specific, confirmed against code) — UNCHANGED

Backend is hardware-agnostic; switching to M5Stack changes nothing here.
- Existing `attendance` table `database.go:182` (person_id + person_type + date +
  class_id, separate check_in/check_out, upsert at `handlers_attendance.go:152`). Reuse it.
- Existing upsert already calls `hub.broadcastTenant(...)` → parents' browsers live-update.
- `students` has no `nfc_uid` yet (`database.go:66`) — migration adds it (unique, nullable).
- New `readers(device_id, tenant_id, secret)` table maps reader → tenant + auth secret.
- Multi-tenancy mandatory: every attendance row carries `tenant_id` (`handlers_students.go:18`).
- Helpers: `respond`/`respondError` (`handlers.go:68`), `logFromReq` (`logger.go:73`),
  `logAudit` (`handlers.go:114`), `generateID` (`handlers.go:180`).
- Public route group near webhooks at `main.go:207`.
- NOTE: old "kiosk/Bluetooth attendance" memory was frontend-only; no device endpoint exists.

## Device management UI (decided 2026-06-17)

Tenant-scoped **Devices tab** in the existing admin (NOT a separate site):
- Reader health: device_id, location, last-seen heartbeat (online/offline), last tap.
- Live tap log: uid / matched student / action / matched class / ts — debugging matchScheduledClass.
- Unknown-UID enrollment queue: one-click assign a tapped UID to a student.
- Admin-only; mask reader secret (last 4), make rotatable. Offline alerts wired into
  existing dashboard health-checks. NO OTA/firmware UI (reflash 1-3 sticks by USB-C).
- Build tenant-scoped so the future platform/superadmin view is a thin aggregation.
  See notes on SaaS trajectory.

## Open item before Step 4 (backend)

Confirm the **classes / schedule schema** (how a class stores day/time/room, how
`students.enrolled_classes` links to it) — `matchScheduledClass` depends on it.

## Build order

1. Set up Arduino IDE + M5Stack board package + M5Unified + M5Unit-NFC libraries.
2. Plug NFC Unit into StickS3 Grove port.
3. **Prove hardware**: flash a UID-to-screen/serial test sketch, tap a band → see the
   UID. Do not proceed until this works.
4. Backend (ME): migration (`nfc_uid` + `readers`) → `handleAttendanceTap` (reuse upsert
   + matchScheduledClass + broadcast + audit) → public route → curl test with
   `{deviceId, uid, secret}` (expect `{"status":"unknown"}` until enrolled).
5. Firmware: add WiFi + POST + screen feedback; edit the 4 config values; flash.
6. Register reader in Devices tab → get device_id/secret → put in firmware → re-flash.
7. Enrol: NFC Tools Android app reads UID → paste into student's nfc_uid field (or use
   the unknown-UID queue after first tap).
8. Mount at kid wrist-height near entrance, power from USB-C wall adapter.

## Known limitations (accepted)

- Buddy-punching (tap a friend's band) — screen showing the wrong name helps staff
  catch it; not worth fully solving for tuition.
- Kids lose/swap bands — keep spares, make re-enrol trivial (unknown-UID queue).
