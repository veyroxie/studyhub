-- 0055_enrolment_start_from_evidence.sql
--
-- Repairs enrolment start dates that record when the ROW was created rather
-- than when the student joined the class.
--
-- WHAT WENT WRONG. `enrolledOn` (utils.js) draws the attendance roster from
-- the dated enrolment stint, so a student is only shown on dates at or after
-- `started_on`. On 2026-08-30 and 08-31 an admin enrolled 35 student/class
-- pairs through the UI, and each row took that day as its start. Every one of
-- those students then vanished from every earlier date -- 85 attendance rows
-- across 19 students, 1 to 28 August, still in the table and invisible on the
-- page. Reported as "Carina, Chase and Luther are in the Wednesday Level 1 & 2
-- class but their August attendance is missing".
--
-- The 0043 backfill is NOT the cause and is not touched here: it derived
-- `started_on` from `students.registered_on` and produced dates back to
-- August 2025. All the damaged rows came from the UI.
--
-- THE RULE. Pull `started_on` back to the earliest attendance record for that
-- student and class, and only when it is earlier. Attendance is evidence the
-- student was in the class that day, so this never invents an enrolment the
-- data does not already support, and it cannot hide a record we hold. Rows
-- with no attendance keep the date they have -- a guess would be worse than
-- an admitted approximation.
--
-- WHY NOW. 0043's own comment says this table is what session billing needs
-- "to prorate mid-month joiners and leavers". Nothing reads `started_on` for
-- money yet, so today this is free. After the pricing switchover the same fix
-- becomes an invoice correction.
--
-- Idempotent: after it runs no EARLIEST stint satisfies `first_att <
-- started_on`. Later stints are deliberately untouched, so a student who left
-- and rejoined still has first-stint attendance sitting before the second
-- stint's start. That is correct, not leftover work.

UPDATE enrollments e
   SET started_on = a.first_att
  FROM (
        SELECT tenant_id, person_id, class_id, MIN(date) AS first_att
          FROM attendance
         WHERE person_type = 'student'
         GROUP BY tenant_id, person_id, class_id
       ) a
 WHERE a.tenant_id  = e.tenant_id
   AND a.person_id  = e.student_id
   AND a.class_id   = e.class_id
   AND a.first_att  < e.started_on
   -- Only the earliest stint. No student holds two stints in one class today,
   -- but backdating a LATER stint would drag it behind the one before it.
   AND e.started_on = (SELECT MIN(e2.started_on) FROM enrollments e2
                        WHERE e2.tenant_id  = e.tenant_id
                          AND e2.student_id = e.student_id
                          AND e2.class_id   = e.class_id);
