-- 0033_retire_boot_alters.sql
--
-- Moves the index + seed statements out of runMigrations() in database.go,
-- which executed on every boot with `db.Exec(m) // intentionally ignore
-- errors`. A failure there — permission, lock timeout, a bad cast — left the
-- app running against a schema it only assumed it had.
--
-- Deliberately NOT carried over: the 13 `ALTER COLUMN ... TYPE NUMERIC`
-- statements. They are already applied by 0002 and 0025, and re-issuing them
-- every boot takes an ACCESS EXCLUSIVE lock on the money tables for no gain.
--
-- Every statement is idempotent, so this is a no-op on the existing database.

INSERT INTO tenants(id,name,subscription_status,plan) VALUES(1,'The Study Hub','active','basic') ON CONFLICT(id) DO NOTHING;
CREATE INDEX IF NOT EXISTS idx_attendance_date ON attendance(date);
CREATE INDEX IF NOT EXISTS idx_invoices_status ON invoices(status);
CREATE INDEX IF NOT EXISTS idx_feedback_date ON feedback(date);
CREATE INDEX IF NOT EXISTS idx_feedback_class ON feedback(class_id);
CREATE INDEX IF NOT EXISTS idx_students_parent ON students(contact);
CREATE INDEX IF NOT EXISTS idx_holidays_date ON holidays(date);
CREATE INDEX IF NOT EXISTS idx_invoices_student ON invoices(student_id);
CREATE INDEX IF NOT EXISTS idx_attendance_person ON attendance(person_id);
CREATE INDEX IF NOT EXISTS idx_feedback_teacher ON feedback(teacher_id);
CREATE INDEX IF NOT EXISTS idx_registrations_status ON registrations(status);
CREATE INDEX IF NOT EXISTS idx_cancelled_classes_class ON cancelled_classes(class_id);
CREATE INDEX IF NOT EXISTS idx_classes_day ON classes(day);
CREATE INDEX IF NOT EXISTS idx_replacement_credits_student ON replacement_credits(student_id);
CREATE INDEX IF NOT EXISTS idx_students_tenant_contact ON students(tenant_id, contact);
CREATE INDEX IF NOT EXISTS idx_invoices_tenant_status ON invoices(tenant_id, status);
CREATE INDEX IF NOT EXISTS idx_classes_tenant_day ON classes(tenant_id, day);
CREATE INDEX IF NOT EXISTS idx_students_family ON students(family_id);
CREATE INDEX IF NOT EXISTS idx_families_contact ON families(contact);
CREATE INDEX IF NOT EXISTS idx_families_tenant ON families(tenant_id);
CREATE INDEX IF NOT EXISTS idx_feedback_replies_feedback ON feedback_replies(feedback_id);
CREATE INDEX IF NOT EXISTS idx_email_tokens_email ON email_tokens(email);
CREATE INDEX IF NOT EXISTS idx_email_tokens_expires ON email_tokens(expires_at) WHERE used_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_families_referral_code ON families(referral_code) WHERE referral_code <> '';
CREATE INDEX IF NOT EXISTS idx_referral_rewards_referrer ON referral_rewards(referrer_family_id);
CREATE INDEX IF NOT EXISTS idx_referral_rewards_student ON referral_rewards(referred_student_id);
CREATE INDEX IF NOT EXISTS idx_attendance_tenant_date ON attendance(tenant_id, date DESC);
CREATE INDEX IF NOT EXISTS idx_attendance_person_date ON attendance(person_id, date DESC);
CREATE INDEX IF NOT EXISTS idx_feedback_replies_tenant ON feedback_replies(tenant_id, feedback_id);
CREATE INDEX IF NOT EXISTS idx_feedback_tenant_date ON feedback(tenant_id, date DESC);
CREATE INDEX IF NOT EXISTS idx_invoices_tenant_student ON invoices(tenant_id, student_id);
CREATE INDEX IF NOT EXISTS idx_progress_reports_student_term ON progress_reports(student_id, term);
CREATE INDEX IF NOT EXISTS idx_progress_reports_tenant ON progress_reports(tenant_id);
CREATE INDEX IF NOT EXISTS idx_mfa_intermediate_expires ON mfa_intermediate(expires_at);
CREATE INDEX IF NOT EXISTS idx_invoices_tenant_deleted ON invoices(tenant_id, deleted_at);
CREATE INDEX IF NOT EXISTS idx_students_tenant_deleted ON students(tenant_id, deleted_at);
CREATE INDEX IF NOT EXISTS idx_classes_tenant_deleted ON classes(tenant_id, deleted_at);
CREATE INDEX IF NOT EXISTS idx_staff_tenant_deleted ON staff(tenant_id, deleted_at);
CREATE INDEX IF NOT EXISTS idx_families_tenant_deleted ON families(tenant_id, deleted_at);
CREATE INDEX IF NOT EXISTS idx_attendance_tenant ON attendance(tenant_id);
CREATE INDEX IF NOT EXISTS idx_feedback_tenant_deleted ON feedback(tenant_id, deleted_at);
CREATE INDEX IF NOT EXISTS idx_announcements_tenant ON announcements(tenant_id);
CREATE INDEX IF NOT EXISTS idx_payroll_tenant ON payroll(tenant_id);
CREATE INDEX IF NOT EXISTS idx_workshops_tenant_deleted ON workshops(tenant_id, deleted_at);
CREATE INDEX IF NOT EXISTS idx_holidays_tenant_deleted ON holidays(tenant_id, deleted_at);
CREATE INDEX IF NOT EXISTS idx_replacement_credits_tenant ON replacement_credits(tenant_id);
CREATE INDEX IF NOT EXISTS idx_cancelled_classes_tenant ON cancelled_classes(tenant_id);
CREATE INDEX IF NOT EXISTS idx_self_study_tenant_deleted ON self_study_sessions(tenant_id, deleted_at);
CREATE INDEX IF NOT EXISTS idx_progress_reports_tenant_deleted ON progress_reports(tenant_id, deleted_at);
CREATE INDEX IF NOT EXISTS idx_performance_reviews_tenant_deleted ON performance_reviews(tenant_id, deleted_at);
