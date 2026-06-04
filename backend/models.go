package main

import (
	"database/sql"
	"encoding/json"
)

// nullStr safely scans a nullable SQL string
func nullStr(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}

// jsonArr marshals a string slice to JSON for storage
func jsonArr(v []string) string {
	if v == nil {
		v = []string{}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// parseArr unmarshals a JSON string into a string slice
func parseArr(s string) []string {
	var out []string
	if s == "" {
		return []string{}
	}
	_ = json.Unmarshal([]byte(s), &out)
	if out == nil {
		return []string{}
	}
	return out
}

// ─── Domain models (JSON tags match frontend App.DATA shape exactly) ──────────

type Student struct {
	ID              string   `json:"id"`
	FirstName       string   `json:"firstName"`
	LastName        string   `json:"lastName"`
	DOB             string   `json:"dob"`
	Gender          string   `json:"gender"`
	ParentName      string   `json:"parentName"`
	Contact         string   `json:"contact"`
	Phone           string   `json:"phone"`
	Branch          string   `json:"branch"`
	Status          string   `json:"status"`
	RegisteredOn    string   `json:"registeredOn"`
	EnrolledClasses []string `json:"enrolledClasses"`
	Siblings        []string `json:"siblings"`
	Notes           string   `json:"notes"`
	Emergency2Name  string   `json:"emergency2Name,omitempty"`
	Emergency2Phone string   `json:"emergency2Phone,omitempty"`
	MedicalInfo     string   `json:"medicalInfo,omitempty"`
	Allergies           string   `json:"allergies,omitempty"`
	FamilyID            string   `json:"familyId,omitempty"`
	ReferredByFamilyID  string   `json:"referredByFamilyId,omitempty"`

	PackageAmount         float64 `json:"packageAmount"`
	PackageSelfStudyHours int     `json:"packageSelfStudyHours"`
	SubscriptionStatus    string  `json:"subscriptionStatus"`
	// DropinSelfStudy marks a casual pay-per-session student (not on the monthly
	// package). Only these appear in the manual self-study invoice picker, so a
	// package student — already auto-billed for overflow by the cron — can't be
	// manually billed and double-charged.
	DropinSelfStudy bool `json:"dropinSelfStudy"`
	PausedAt              *string `json:"pausedAt,omitempty"`
	ResumedAt             *string `json:"resumedAt,omitempty"`
}

type Family struct {
	ID                       string `json:"id"`
	Name                     string `json:"name"`
	Contact                  string `json:"contact"`
	Phone                    string `json:"phone"`
	ParentName               string `json:"parentName"`
	Address                  string `json:"address,omitempty"`
	Notes                    string `json:"notes,omitempty"`
	ReferralCode             string `json:"referralCode,omitempty"`
	ReferralCreditsRemaining int    `json:"referralCreditsRemaining"`
}

type Class struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	TeacherIDs []string `json:"teacherIds"`
	Classroom  string   `json:"classroom"`
	Day        string   `json:"day"`
	Time       string   `json:"time"`
	EndTime    string   `json:"endTime"`
	Capacity   int      `json:"capacity"`
	Enrolled   int      `json:"enrolled"`
	Color      string   `json:"color"`
	Category   string   `json:"category"`
}

type Staff struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	FullName       string  `json:"fullName"`
	Role           string  `json:"role"`
	Email          string  `json:"email"`
	Phone          string  `json:"phone"`
	Salary         float64 `json:"salary"`
	JoinDate       string  `json:"joinDate"`
	Status         string  `json:"status"`
	Specialization string  `json:"specialization,omitempty"`
	NRIC           string  `json:"nric,omitempty"`
	EmergencyName  string  `json:"emergencyName,omitempty"`
	EmergencyPhone string  `json:"emergencyPhone,omitempty"`
	EmploymentType string  `json:"employmentType,omitempty"`
	HourlyRate     float64 `json:"hourlyRate,omitempty"`
}

type Invoice struct {
	ID                string  `json:"id"`
	StudentID         string  `json:"studentId"`
	Description       string  `json:"description"`
	Type              string  `json:"type"`
	Amount            float64 `json:"amount"`
	DueDate           string  `json:"dueDate"`
	Status            string  `json:"status"`
	CreatedOn         string  `json:"createdOn"`
	PaidOn            *string `json:"paidOn"`
	PaymentProof      string  `json:"paymentProof"`
	PaymentMethod     string  `json:"paymentMethod"`
	DiscountPct       float64 `json:"discountPct"`
	SubmittedByParent bool    `json:"submittedByParent"`
	SiblingIds        string  `json:"siblingIds"`
	SiblingDiscount   float64 `json:"siblingDiscount"`
	ReferralCredit    float64 `json:"referralCredit"`
	ReferenceNo       string  `json:"referenceNo"`
	DeletedAt         *string `json:"deletedAt,omitempty"`
}

type Announcement struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Message   string `json:"message"`
	Audience  string `json:"audience"`
	Type      string `json:"type"`
	Status    string `json:"status,omitempty"`
	ArchiveOn string `json:"archiveOn,omitempty"`
	CreatedOn string `json:"createdOn"`
	CreatedBy string `json:"createdBy"`
}

type Attendance struct {
	ID         string  `json:"id"`
	PersonID   string  `json:"personId"`
	PersonType string  `json:"personType"`
	Date       string  `json:"date"`
	ClassID    *string `json:"classId"`
	CheckIn    *string `json:"checkIn"`
	CheckOut   *string `json:"checkOut"`
	Status     string  `json:"status"`
}

type Payroll struct {
	ID         string  `json:"id"`
	StaffID    string  `json:"staffId"`
	Month      string  `json:"month"`
	BaseSalary float64 `json:"baseSalary"`
	Bonus      float64 `json:"bonus"`
	Deductions float64 `json:"deductions"`
	Total      float64 `json:"total"`
	Status     string  `json:"status"`
	PaidOn     *string `json:"paidOn"`
}

type Registration struct {
	ID                string  `json:"id"`
	ParentName        string  `json:"parentName"`
	Email             string  `json:"email"`
	Phone             string  `json:"phone"`
	EmergencyName     string  `json:"emergencyName"`
	EmergencyPhone    string  `json:"emergencyPhone"`
	StudentFirstName  string  `json:"studentFirstName"`
	StudentLastName   string  `json:"studentLastName"`
	StudentDOB        string  `json:"studentDob"`
	StudentGender     string  `json:"studentGender"`
	Gender            string  `json:"gender"`
	SchoolName        string  `json:"schoolName"`
	YearGrade         string  `json:"yearGrade"`
	ClassTypeInterest string  `json:"classTypeInterest"`
	SubjectInterest   string  `json:"subjectInterest"`
	SchoolFees        float64 `json:"schoolFees"`
	RegistrationDate  string  `json:"registrationDate"`
	WorkshopInterest  string  `json:"workshopInterest"`
	ClassInterest     string  `json:"classInterest"`
	Notes             string  `json:"notes"`
	SubmittedOn       string  `json:"submittedOn"`
	Status            string  `json:"status"` // pending | approved | rejected
	Type              string  `json:"type"`   // "student" or "teacher"
	Specialization    string  `json:"specialization"`
	NRIC              string  `json:"nric"`
	DisplayName       string  `json:"displayName"`
	EmploymentType    string  `json:"employmentType"`
	Experience        string  `json:"experience"`
	Qualifications    string  `json:"qualifications"`
	Bio               string  `json:"bio"`
	Schedule          string  `json:"schedule"`
	ExpectedSalary    string  `json:"expectedSalary"`
	ReferralCode      string  `json:"referralCode,omitempty"`
	// EmailVerifiedAt is non-empty when the parent (or teacher applicant)
	// has clicked the verification link in their welcome email. Drives the
	// "verified" badge on the admin pending list.
	EmailVerifiedAt string `json:"emailVerifiedAt,omitempty"`
}

type ReferralReward struct {
	ID                string `json:"id"`
	ReferrerFamilyID  string `json:"referrerFamilyId"`
	ReferredStudentID string `json:"referredStudentId"`
	Status            string `json:"status"` // pending | earned | exhausted
	PaidInvoiceCount  int    `json:"paidInvoiceCount"`
	CreditsRemaining  int    `json:"creditsRemaining"`
	MilestoneMetOn    string `json:"milestoneMetOn,omitempty"`
	CreatedAt         string `json:"createdAt,omitempty"`
	// Joined display fields (filled by listReferralRewards)
	ReferrerName     string `json:"referrerName,omitempty"`
	ReferredName     string `json:"referredName,omitempty"`
}

type StudentNote struct {
	StudentID string `json:"studentId"`
	Note      string `json:"note"`
}

type Feedback struct {
	ID           string        `json:"id"`
	ClassID      string        `json:"classId"`
	Date         string        `json:"date"`
	TeacherID    string        `json:"teacherId"`
	Topic        string        `json:"topic"`
	Mood         string        `json:"mood"`
	Notes        string        `json:"notes"`
	StudentNotes []StudentNote `json:"studentNotes"`
}

type Subject struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Category    string  `json:"category"`
	Level       string  `json:"level"`
	Description string  `json:"description"`
	MonthlyFee  float64 `json:"monthlyFee"`
	Color       string  `json:"color"`
}

type Workshop struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Date        string   `json:"date"`
	Time        string   `json:"time"`
	EndTime     string   `json:"endTime"`
	Classroom   string   `json:"classroom"`
	Capacity    int      `json:"capacity"`
	Enrolled    int      `json:"enrolled"`
	Fee         float64  `json:"fee"`
	TeacherIDs  []string `json:"teacherIds"`
	Status      string   `json:"status"`
}

type SelfStudySession struct {
	ID          string `json:"id"`
	StudentID   string `json:"studentId"`
	Date        string `json:"date"`
	StartTime   string `json:"startTime"`
	EndTime     string `json:"endTime"`
	DurationMin int    `json:"durationMin"`
	Notes       string `json:"notes"`
}

type PerformanceReview struct {
	ID            string  `json:"id"`
	StaffID       string  `json:"staffId"`
	ReviewerEmail string  `json:"reviewerEmail"`
	Date          string  `json:"date"`
	Rating        float64 `json:"rating"`
	ParentRating  float64 `json:"parentRating"`
	Notes         string  `json:"notes"`
}

type CancelledClass struct {
	ID          string `json:"id"`
	ClassID     string `json:"classId"`
	Date        string `json:"date"`
	Reason      string `json:"reason"`
	CancelledBy string `json:"cancelledBy"`
	CreatedOn   string `json:"createdOn"`
}

type Holiday struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Date      string `json:"date"`
	EndDate   string `json:"endDate,omitempty"`
	Type      string `json:"type"`
	Notes     string `json:"notes,omitempty"`
	CreatedBy string `json:"createdBy,omitempty"`
}

type ReplacementCredit struct {
	ID        string `json:"id"`
	StudentID string `json:"studentId"`
	Type      string `json:"type"`
	Minutes   int    `json:"minutes"`
	Note      string `json:"note"`
	ClassID   string `json:"classId"`
	Date      string `json:"date"`
	CreatedBy string `json:"createdBy"`
	Category  string `json:"category"` // "class" or "self-study"
}

type FeedbackReply struct {
	ID          string `json:"id"`
	FeedbackID  string `json:"feedbackId"`
	AuthorEmail string `json:"authorEmail"`
	AuthorName  string `json:"authorName"`
	Message     string `json:"message"`
	CreatedAt   string `json:"createdAt"`
}

// ProgressReport is a termly per-student progress note that replaces the
// per-class feedback flow. Reports can be drafts (admin/teacher only) or
// published (visible to parents whose latest invoice is paid).
type ProgressReport struct {
	ID               string `json:"id"`
	StudentID        string `json:"studentId"`
	Term             string `json:"term"`
	TeacherID        string `json:"teacherId"`
	Subject          string `json:"subject"`
	Grade            string `json:"grade"`
	Strengths        string `json:"strengths"`
	AreasToImprove   string `json:"areasToImprove"`
	TeacherComment   string `json:"teacherComment"`
	NextTermFocus    string `json:"nextTermFocus"`
	Published        bool   `json:"published"`
	CreatedAt        string `json:"createdAt"`
	UpdatedAt        string `json:"updatedAt"`
}

// Snapshot is what GET /api/snapshot returns — identical shape to App.DATA
type Snapshot struct {
	Students           []Student           `json:"students"`
	Classes            []Class             `json:"classes"`
	Staff              []Staff             `json:"staff"`
	Invoices           []Invoice           `json:"invoices"`
	Announcements      []Announcement      `json:"announcements"`
	Attendance         []Attendance        `json:"attendance"`
	Payroll            []Payroll           `json:"payroll"`
	Registrations      []Registration      `json:"registrations,omitempty"`
	Feedback           []Feedback          `json:"feedback"`
	Subjects           []Subject           `json:"subjects"`
	Workshops          []Workshop          `json:"workshops"`
	SelfStudySessions  []SelfStudySession  `json:"selfStudySessions"`
	PerformanceReviews []PerformanceReview `json:"performanceReviews"`
	CancelledClasses   []CancelledClass    `json:"cancelledClasses"`
	Holidays           []Holiday           `json:"holidays"`
	ReplacementCredits []ReplacementCredit `json:"replacementCredits"`
	Families           []Family            `json:"families"`
	FeedbackReplies    []FeedbackReply     `json:"feedbackReplies"`
	ReferralRewards    []ReferralReward    `json:"referralRewards"`
	PendingUsers       []PendingUser       `json:"pendingUsers,omitempty"`
	ProgressReports    []ProgressReport    `json:"progressReports"`
}

// PendingUser is a minimal projection of users with status=pending_verification,
// included in the admin snapshot so the dashboard can show action items.
type PendingUser struct {
	ID    int    `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
	Role  string `json:"role"`
}
