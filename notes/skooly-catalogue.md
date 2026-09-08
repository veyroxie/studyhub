# Skooly's price list, as migrated from

Source: Nadine pasted the live Skooly course/pricing screen, 2026-09-07.
This is the system StudyHub replaces. Recorded so the prices can be looked up
here instead of asked for again.

**Read the caveat in section 3 before trusting a number.** Skooly accumulated
pricing OPTIONS per course and never removed the old ones, so several courses
list two or three monthly prices side by side. It is a log of every price ever
offered, not a statement of the current one.

## 1. What it confirms outright

**Prorated weekly = monthly / 4, with no exceptions.** Every course that lists
both agrees:

```
  240/4 = 60      260/4 = 65      320/4 = 80
  400/4 = 100     480/4 = 120     520/4 = 130     780/4 = 195
```

This settles the add-on divisor in `addon-classes.md`, which until now rested
on one Ying Quah quote. "max classes 4" on every 1x plan and "max classes 8" on
every 2x plan says the same thing structurally: a month buys four sessions per
weekly slot.

**Self-study is RM10/hour.** "TSH Membership (Extra Hours) - RM 10.00 for 1
session", and "4 Self-study Hours - RM 40.00" with "class duration 30 min, max
classes 8". Matches `SelfStudyOverflowRatePerHour` and migration 0054's seed.

**Memberships are the `package_amount` students.** 4 hours RM40, 8 hours RM80,
Standard RM100 (max 10 classes), Premium RM300.

## 2. Prices we did not have

| Skooly course | Monthly | Weekly | Our catalogue |
| --- | --- | --- | --- |
| Singapore Math (Lvl 0) - Private | 320 | 80 | **missing -- no Level 0 tier** |
| Mandarin - Group | 240 | 60 | **missing** |
| Mandarin - Private | 320 and 400 | 80 | **missing** |
| English - Private | 400 | 100 | **missing** |

**Mandarin - Group at RM240 unblocks Luther's retroactive September invoice**,
which was the last thing waiting on Nadine. Chase, Zayden and Luther are all in
the Thursday Mandarin group.

**Level 0 Private at RM320 (RM80/week) is Aria's price**, and matches the
`session_rate` of 80 already on `Teacher Chiying (Aria)`, which Nadine
confirmed 09-08 as correct.

## 3. Where Skooly disagrees with our seeded catalogue

Do NOT copy these across without asking. Every conflict is Skooly holding an
old price beside a new one:

| Tier | Skooly lists | We seeded | Note |
| --- | --- | --- | --- |
| Group 3-4, 1x | 240 **and** 260 | 260 | 240 is the old Lvl 1-2 rate |
| Group 3-4, 2x | 450 | 490 | 450 = 2x240-30, so it is the OLD rate's twice-weekly |
| Group 5-6, 2x | 1,010 | 490 | 1,010 is a PRIVATE 3-4 price; looks like a mis-entry |
| Private 3-4, 1x | 480 **and** 520 | 520 | 480 is the old Lvl 1-2 rate |
| Private 3-4, 2x | 930 **and** 1,010 | 1,010 | 930 = 2x480-30 |
| Private 5-6, 1x | 520 **and** 780 | 520 | 780 (195/wk) is unexplained |

The pattern is legible: the twice-weekly figures track `2 x weekly - 30`
against whichever weekly rate was current when they were entered. Our seed used
the newer rates, which is right. The two genuinely open ones are **Group 5-6 2x
(1,010 vs 490)** and **Private 5-6 1x (780 vs 520)**.

## 4. Aria and Aleena, reconciled

Nadine 09-08: "Yes for Aria. Aleena would be RM90 for an 1 1/2hours."

Both statements check out against production, and neither needs a fix:

```
  Aria    Teacher Chiying (Aria)   Sat 09:00-10:00   session_rate 80   -> Lvl 0 Private, 320/mo
          Teacher Chiying (Aria)   Tue 15:30-16:30   session_rate  0   -> second slot, unpriced

  Aleena  Phonics (Aria & Aleena)  Sat 10:30-11:30   1.0 h  \  1.5 h back to back
          Teacher Nadine (Aleena)  Sat 11:30-12:00   0.5 h  /  RM90 = package_amount 360 / 4
```

**The RM30 on Aleena's class is not wrong.** It is a 30-MINUTE session, so RM30
is RM60/hour, and her Saturday block of 1.5 hours comes to RM90 -- exactly what
Nadine said, and exactly her stored `package_amount` of 360 (4 x 90). Section 9
of `pricing-bands.md` listed this as a stray rate to question; it is correct and
should be closed.

Two things this DOES surface:

- **Aria's Tuesday slot has no price.** She holds two live Private enrolments
  and Skooly has no twice-weekly Level 0 price. Either the second slot is
  RM80 as well (640/month) or there is a two-slot rate nobody has stated.
- **Aria is not enrolled in the class named after her.** `Phonics (Aria &
  Aleena)` has one live student, Aleena. Either the name is a leftover or an
  enrolment is missing.

## 5. To double-check with Nadine

Ordered by what blocks work.

1. **Group 5-6 twice-weekly: 1,010 or 490?** Skooly's 1,010 equals the Private
   3-4 price, which smells like a mis-entry, but a group paying a private rate
   is not impossible. Blocks nothing today (no 5-6 group students).
2. **Private 5-6: 520 or 780?** 780 prorates to 195/week and appears only as
   manual billing. Geneva, Zia and Carolina are all Level 5.
3. **Aria's second slot** -- is Tuesday another RM80, or is there a
   twice-weekly Level 0 rate?
4. **Phonics at 239.96.** The stored `monthly_fee_override` is four sen short
   of 240. Cosmetic while Aleena is on a package, wrong the moment she is not.
5. **Mandarin - Private, 320 or 400?** Not blocking: all three current Mandarin
   students are in the group class.
6. **English - Private (400)** -- is this a live product? No class in StudyHub
   references it.

## 6. Answered by the group chat, 08-13 to 08-17

Found in the WhatsApp export, so these needed no new question:

**Phonics is RM60 per hour, group only, no level** (Nadine, 08-17: "Phonics
will also cost RM60 per hour for group. No private. No level for phonics").
`Phonics (Aria & Aleena)` runs Saturday 10:30-11:30, one hour, so RM60/session
and RM240/month.

**Its stored `monthly_fee_override` of 239.96 is therefore wrong, not merely
odd.** It should be 240.00. Four sen, invisible while Aleena is on a package,
real the moment she is not.

**The RM30 thirty-minute Math class is Aleena's, and she is Level 0** (Nadine,
08-17: "only one specific class. The girl is currently level 0"), answering the
request from 08-13 for "Math group class (30 minutes options for RM30)".

Aleena's week reconciles exactly against her `package_amount`:

```
  Phonics            1.0 h @ RM60/h = RM60   (group hourly rate)
  Math, 30 min       0.5 h           = RM30
                                       ----
                                       RM90 x 4 weeks = RM360 = package_amount
```

**A note on the category flag raised earlier.** `Teacher Nadine (Aleena)` sits
in `PC_group` despite being one-to-one, which `pricing-bands.md` section 12c
called a miscategorisation. It is priced at the GROUP hourly rate of RM60,
so the row is internally consistent and the derived sessions-per-week concern
still applies but the price does not contradict the category. Whether a 1-to-1
class should charge the group rate is Nadine's call, not a data fault.

## 7. Still unanswered after the chat export

Neither the Skooly list nor the full group chat resolves these:

1. Group 5-6 twice-weekly: 1,010 or 490?
2. Private 5-6: 520 or 780?
3. Aria's second (Tuesday) Level 0 slot -- another RM80, or a two-slot rate?
4. Why `Phonics (Aria & Aleena)` has no Aria enrolment.
