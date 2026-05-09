# Real-time check-in / check-out — hardware options

The goal is a check-in flow at the centre door that triggers an immediate parent notification, doesn't put a phone in a kid's hand, and doesn't require staff to babysit the device. Today we have a kiosk tab in the attendance module that accepts scanner input via the keyboard buffer, but no hardware committed.

Three options ordered by recommendation.

## Recommended: tablet kiosk + printed QR cards

- **Hardware**: one cheap Android tablet (Lenovo Tab M9 ~RM500 or any 8–10" Android with Wi-Fi) wall-mounted near the entrance. Charge it on a permanent power adapter. ~RM800 all-in including mount and charger.
- **Software**: open the existing kiosk tab in Chrome. Set Chrome to "kiosk mode" / fullscreen. Put it in Android's screen-pinning mode so kids can't navigate away.
- **Inputs**: 
  - **Tap a child's name** on a tile, OR
  - **Scan a printed QR card** the child carries on their bag — same backend; the kiosk listens for keyboard-style input from a USB QR scanner (~RM150) for the brave-new-world version.
- **Notifications**: WebSocket already broadcasts CHECK_IN / CHECK_OUT events. The parent app picks them up over the existing connection (real-time, no per-device pairing).
- **Cost recap**: ~RM800 tablet + RM150 optional scanner + ~RM30 to print 50 student QR cards on PVC = ~RM1000 one-off, RM0/month.

This is what the existing kiosk code is built for. No new code needed beyond polishing the kiosk UI for a wall-mount viewing distance (bigger tap targets, larger fonts).

## QR scanner only (no tablet)

- USB QR scanner plugged into an existing laptop at reception (~RM150).
- Same kiosk page open.
- Cheaper, but requires a laptop already at reception running Chrome, and someone to keep it from being closed.
- Same backend.

Good as a fallback if the tablet budget isn't there. Same RM30 PVC cards.

## NFC wristbands

- Per-child wristband or sticker, ~RM5 each.
- USB or USB-C NFC reader (~RM200, fewer reliable consumer options).
- Owner already noticed in the field that NFC readers need to maintain a paired connection that drops; rebuilding the pairing each session is friction reception staff don't have time for.
- The backend would need an `nfc_uid` column on students and a small handler to map the UID to a student.

Not recommended. The hardware story is shakier than QR + tablet, the cost-per-child is real (RM5 × headcount), and replacement cost when a wristband is lost falls on the family.

## Decision

Buy a tablet + USB QR scanner + print PVC cards. Mount the tablet at the door, run the existing kiosk page fullscreen. Total spend ~RM1000 one-off. Time-to-deploy: a weekend.
