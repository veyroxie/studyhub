# NFC Wristband Attendance — Plan

Status: **planning** (no code written yet). Hardware not yet purchased.
Decided 2026-06-17.

## Goal

Kids tap an NTAG213 silicone wristband on a reader at the centre; the system
records check-in / check-out against StudyHub's existing `attendance` table and
lights green/red for instant feedback. No screen — LEDs + buzzer are the UI.

## Hardware (one reader per entry point)

- ESP32 dev board (ESP32-WROOM-32 / DevKitC) — WiFi built in, 2.4GHz only
- PN532 NFC module (DIP switches set to I2C) — supports NTAG; RC522 would not
- 1× active buzzer, 1× green LED, 1× red LED, 2× ~220Ω resistors
- Female-female jumper wires, 5V USB wall adapter, enclosure
- NTAG213 silicone wristbands (bulk + spares) — only the 7-byte UID is used

Wiring: PN532 VCC→3V3, GND→GND, SDA→GPIO21, SCL→GPIO22, IRQ→GPIO4, RSTO→GPIO5.
LEDs green→GPIO25, red→GPIO26 (each via resistor to GND). Buzzer→GPIO27.

## Key design decisions

1. **Auto-match scheduled class** — a tap attaches the `class_id` of the class
   the student is enrolled in and scheduled around tap time, NOT a flat
   building log. Falls back to `class_id NULL` (building presence) if no class
   is in window.
   - Match window: `[start − 30min, end + 30min]`.
   - Ambiguity (overlapping classes): pick start nearest tap time; log it.
2. **One reader toggles in/out** — first tap for a `(student, today, class_id)`
   = `check_in`; second = `check_out`. Toggle is **per matched class**, so
   back-to-back classes each get their own in/out.
3. **Server-side debounce** — ignore the same `(uid, class_id)` within ~5s so an
   accidental double-tap doesn't instantly check the kid back out. Do NOT trust
   firmware timing for this.
4. **Device-token auth, public route** — `/tap` is a public endpoint (ESP32 has
   no user login). Mirror the iCal token pattern (`handlers_ical.go:77`):
   validate a per-device secret, derive `tenant_id`, build synthetic context.
   Do NOT put it behind the user-JWT cookie middleware.

## Backend integration (StudyHub-specific, confirmed against code)

- Existing `attendance` table: `backend/database.go:182` — keyed on
  `person_id` + `person_type` + `date` + `class_id`, separate `check_in` /
  `check_out`, upsert handler at `handlers_attendance.go:152`. **Reuse it.**
- Existing upsert already calls `hub.broadcastTenant(...)` → parents' browsers
  live-update on check-in/out for free.
- `students` has **no `nfc_uid`** column yet (`database.go:66`) — migration adds
  `nfc_uid` (unique, nullable TEXT).
- New `readers(device_id, tenant_id, secret)` table to map a physical reader to
  a tenant + auth secret.
- Multi-tenancy is mandatory: every attendance row carries `tenant_id`
  (`handlers_students.go:18`, `scopeTenant` at `:42`).
- Helpers: `respond` / `respondError` (`handlers.go:68`), `logFromReq`
  (`logger.go:73`), `logAudit` (`handlers.go:114`), `generateID` (`handlers.go:180`).
- Route registration: public group near webhooks at `main.go:207`.
- NOTE: the "kiosk/Bluetooth attendance" in old memory was frontend-only — there
  is NO existing device endpoint. This is built fresh.

## Open item before Step 4

Need to confirm the **classes / schedule schema** (how a class stores
day/time/room, how `students.enrolled_classes` links to it) — the `matchScheduledClass`
logic depends on it. Confirm before writing the migration.

## Build order

1. Dev env (Arduino IDE + ESP32 board pkg + Adafruit PN532 lib)
2. Wire it up
3. **Prove hardware**: flash UID-to-serial sketch, tap → see UID at 115200 baud.
   Do not proceed until this works.
4. Backend: migration (`nfc_uid` + `readers`) → `handleAttendanceTap` (reuse
   upsert + matchScheduledClass + broadcast + audit) → public route → curl test
   with `{deviceId, uid, secret}` (expect `{"status":"unknown"}` until enrolled).
5. Firmware WiFi + POST (add `deviceId`/`secret` to body).
6. LED/buzzer feedback — two green patterns from `action: "checkin"|"checkout"`.
7. Enrol: NFC Tools Android app reads UID → paste into a new admin field bound
   to `students.nfc_uid`.
8. Enclosure + wall-height mount.

## Known limitations (accepted)

- Buddy-punching (kid taps a friend's band) — fine for tuition, not worth solving.
- Kids lose/swap bands — keep spares, make re-enrol trivial.
