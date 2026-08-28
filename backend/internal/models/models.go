package models

import (
	"database/sql"
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

// Invoice line-item kinds. "item" is a positive charge; "discount" is a
// negative line (included-self-study FOC, sibling, referral, early-bird).
const (
	LineItemKindItem     = "item"
	LineItemKindDiscount = "discount"
)

// nullStr safely scans a nullable SQL string
func NullStr(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}

// jsonArr marshals a string slice to JSON for storage
func JSONArr(v []string) string {
	if v == nil {
		v = []string{}
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// parseArr unmarshals a JSON string into a string slice
func ParseArr(s string) []string {
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

// MarshalLineItems serialises invoice line items to the JSON stored in
// invoices.line_items. Mirrors JSONArr — never fails the caller, empty array
// on nil or marshal error.
func MarshalLineItems(items []InvoiceLineItem) string {
	if items == nil {
		return "[]"
	}
	b, err := json.Marshal(items)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// ParseLineItems reads the invoices.line_items JSON back into line items.
// Tolerates "" and "[]" for legacy flat invoices. Mirrors ParseArr.
func ParseLineItems(s string) []InvoiceLineItem {
	if s == "" {
		return []InvoiceLineItem{}
	}
	var out []InvoiceLineItem
	_ = json.Unmarshal([]byte(s), &out)
	if out == nil {
		return []InvoiceLineItem{}
	}
	return out
}

// round2 rounds a ringgit amount to the sen (2 dp) so line totals don't drift.
func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

// NormalizeLineItems recomputes each item's Amount server-side so the client
// can never dictate the invoice total: "item" lines become Qty*UnitPrice;
// "discount" lines keep their Amount clamped to <= 0. Returns the summed total,
// which the caller stores as the authoritative invoices.amount.
func NormalizeLineItems(items []InvoiceLineItem) float64 {
	total := 0.0
	for i := range items {
		if items[i].Kind == LineItemKindDiscount {
			items[i].Amount = round2(math.Min(0, items[i].Amount))
		} else {
			items[i].Amount = round2(items[i].Qty * items[i].UnitPrice)
		}
		total += items[i].Amount
	}
	return round2(total)
}

// LineItemsSummary builds a one-line description from the item names, used when
// a multi-line invoice arrives without an explicit description.
func LineItemsSummary(items []InvoiceLineItem) string {
	names := make([]string, 0, len(items))
	for _, it := range items {
		if it.Kind == LineItemKindItem && it.Name != "" {
			names = append(names, it.Name)
		}
	}
	if len(names) == 0 {
		return "Invoice"
	}
	return strings.Join(names, ", ")
}

// FormatQty renders a line-item quantity without a trailing ".00" for whole
// numbers (e.g. "5" not "5.00", but "1.5" stays).
func FormatQty(qty float64) string {
	return strconv.FormatFloat(qty, 'f', -1, 64)
}

// ─── Domain models (JSON tags match frontend App.DATA shape exactly) ──────────

type Student struct {
	ID                 string   `json:"id"`
	StudentNo          string   `json:"studentNo"`
	FirstName          string   `json:"firstName"`
	LastName           string   `json:"lastName"`
	DOB                string   `json:"dob"`
	Gender             string   `json:"gender"`
	ParentName         string   `json:"parentName"`
	Contact            string   `json:"contact"`
	Phone              string   `json:"phone"`
	Branch             string   `json:"branch"`
	Status             string   `json:"status"`
	RegisteredOn       string   `json:"registeredOn"`
	EnrolledClasses    []string `json:"enrolledClasses"`
	Siblings           []string `json:"siblings"`
	Notes              string   `json:"notes"`
	Emergency2Name     string   `json:"emergency2Name,omitempty"`
	Emergency2Phone    string   `json:"emergency2Phone,omitempty"`
	MedicalInfo        string   `json:"medicalInfo,omitempty"`
	Allergies          string   `json:"allergies,omitempty"`
	FamilyID           string   `json:"familyId,omitempty"`
	ReferredByFamilyID string   `json:"referredByFamilyId,omitempty"`

	// LevelBand is the student's OWN pricing band ('1-3' | '4-6'), for mixed
	// classes straddling the boundary. '' = use the class's band (0045).
	LevelBand string `json:"levelBand"`

	PackageAmount         float64 `json:"packageAmount"`
	PackageSelfStudyHours int     `json:"packageSelfStudyHours"`
	SubscriptionStatus    string  `json:"subscriptionStatus"`
	// DropinSelfStudy marks a casual pay-per-session student (not on the monthly
	// package). Only these appear in the manual self-study invoice picker, so a
	// package student — already auto-billed for overflow by the cron — can't be
	// manually billed and double-charged.
	DropinSelfStudy bool `json:"dropinSelfStudy"`
	// Why and when a student stopped attending, for retention analysis.
	InactiveReason string  `json:"inactiveReason,omitempty"`
	InactiveOn     string  `json:"inactiveOn,omitempty"`
	PausedAt       *string `json:"pausedAt,omitempty"`
	ResumedAt      *string `json:"resumedAt,omitempty"`
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
	// Subject is a label (Math, Mandarin) shown on the invoice line. It is
	// deliberately NOT part of the pricing key — see migration 0034.
	Subject   string `json:"subject"`
	ClassType string `json:"classType"` // 'Group' | 'Private' — drives pricing tier + capacity
	LevelBand string `json:"levelBand"` // '1-3' | '4-6' — drives pricing tier
	// MonthlyFeeOverride wins over the (classType, levelBand) matrix when > 0.
	// Exists for classes the matrix cannot price — Phonics has no level band,
	// and a 30-minute group runs below the Level 1 rate. See migration 0037.
	MonthlyFeeOverride float64 `json:"monthlyFeeOverride"`
	// SessionRate prices ONE session of this class outright (0045), winning
	// over the (classType, band) hourly matrix. 0 = unset.
	SessionRate float64 `json:"sessionRate"`
}

// PricingTier is one cell of the type×level fee matrix. The monthly cron looks
// up a class's (classType, levelBand) here to price each enrolled student.
type PricingTier struct {
	ID         string  `json:"id"`
	ClassType  string  `json:"classType"`
	LevelBand  string  `json:"levelBand"`
	MonthlyFee float64 `json:"monthlyFee"`
	// HourlyRate prices one hour of a session (0045). Session billing bills
	// hourly_rate x class duration; MonthlyFee stays until the F5 switchover.
	HourlyRate float64 `json:"hourlyRate"`
}

type Staff struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	FullName         string  `json:"fullName"`
	Role             string  `json:"role"`
	Email            string  `json:"email"`
	Phone            string  `json:"phone"`
	Salary           float64 `json:"salary"`
	JoinDate         string  `json:"joinDate"`
	Status           string  `json:"status"`
	Specialization   string  `json:"specialization,omitempty"`
	NRIC             string  `json:"nric,omitempty"`
	EmergencyName    string  `json:"emergencyName,omitempty"`
	EmergencyPhone   string  `json:"emergencyPhone,omitempty"`
	EmploymentType   string  `json:"employmentType,omitempty"`
	HourlyRate       float64 `json:"hourlyRate,omitempty"`
	PerformanceNotes string  `json:"performanceNotes,omitempty"`
}

// InvoiceLineItem is one row of a multi-line invoice, stored as a JSON array in
// invoices.line_items. The invoice's Amount stays the authoritative total,
// derived server-side from the sum of these items. Kind "discount" carries a
// negative Amount (e.g. an included-self-study FOC line or early-bird).
type InvoiceLineItem struct {
	Kind        string   `json:"kind"`        // "item" | "discount"
	Name        string   `json:"name"`        // bold heading, e.g. "Group Level 1-3 Tuition"
	Descriptor  string   `json:"descriptor"`  // gray sub-line under the name
	PeriodStart string   `json:"periodStart"` // "2026-07-01", optional
	PeriodEnd   string   `json:"periodEnd"`   // "2026-07-31", optional
	Qty         float64  `json:"qty"`
	UnitPrice   float64  `json:"unitPrice"`
	Amount      float64  `json:"amount"`            // qty*unitPrice; negative for a discount line
	Details     []string `json:"details,omitempty"` // extra gray detail sub-lines
}

type Invoice struct {
	ID                string            `json:"id"`
	StudentID         string            `json:"studentId"`
	Description       string            `json:"description"`
	Type              string            `json:"type"`
	Amount            float64           `json:"amount"`
	DueDate           string            `json:"dueDate"`
	Status            string            `json:"status"`
	CreatedOn         string            `json:"createdOn"`
	PaidOn            *string           `json:"paidOn"`
	PaymentProof      string            `json:"paymentProof"`
	PaymentMethod     string            `json:"paymentMethod"`
	DiscountPct       float64           `json:"discountPct"`
	SubmittedByParent bool              `json:"submittedByParent"`
	SiblingIds        string            `json:"siblingIds"`
	SiblingDiscount   float64           `json:"siblingDiscount"`
	ReferralCredit    float64           `json:"referralCredit"`
	ReferenceNo       string            `json:"referenceNo"`
	EarlyBirdCutoff   string            `json:"earlyBirdCutoff"`
	EarlyBirdDiscount float64           `json:"earlyBirdDiscount"`
	LineItems         []InvoiceLineItem `json:"lineItems"`
	DeletedAt         *string           `json:"deletedAt,omitempty"`
}

type Announcement struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Message  string `json:"message"`
	Audience string `json:"audience"`
	// TargetClassIDs limits visibility to parents of these classes; empty
	// means the audience string alone decides (e.g. "All Parents").
	TargetClassIDs []string `json:"targetClassIds,omitempty"`
	Type           string   `json:"type"`
	Status         string   `json:"status,omitempty"`
	ArchiveOn      string   `json:"archiveOn,omitempty"`
	CreatedOn      string   `json:"createdOn"`
	CreatedBy      string   `json:"createdBy"`
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
	// ManuallyEdited marks a row the admin corrected by hand; the payroll
	// recompute (cron + regenerate endpoint) never overwrites such rows.
	ManuallyEdited bool `json:"manuallyEdited"`
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
	ReferrerName string `json:"referrerName,omitempty"`
	ReferredName string `json:"referredName,omitempty"`
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

// SessionOverride is one dated session that differs from its recurring class.
// Absence of a row means the class template applies, so swapping a teacher for
// one week leaves every other week alone. See migration 0040.
type SessionOverride struct {
	ID         string   `json:"id"`
	ClassID    string   `json:"classId"`
	Date       string   `json:"date"`
	TeacherIDs []string `json:"teacherIds"`
	Note       string   `json:"note"`
	CreatedBy  string   `json:"createdBy"`
	CreatedOn  string   `json:"createdOn"`
}

// SessionMove relocates one dated session of a recurring class to another
// date for all students. No credits are granted: the session still happens.
type SessionMove struct {
	ID        string `json:"id"`
	ClassID   string `json:"classId"`
	FromDate  string `json:"fromDate"`
	ToDate    string `json:"toDate"`
	Reason    string `json:"reason"`
	MovedBy   string `json:"movedBy"`
	CreatedOn string `json:"createdOn"`
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
	ID             string `json:"id"`
	StudentID      string `json:"studentId"`
	Term           string `json:"term"`
	TeacherID      string `json:"teacherId"`
	Subject        string `json:"subject"`
	Grade          string `json:"grade"`
	Strengths      string `json:"strengths"`
	AreasToImprove string `json:"areasToImprove"`
	TeacherComment string `json:"teacherComment"`
	NextTermFocus  string `json:"nextTermFocus"`
	Published      bool   `json:"published"`
	CreatedAt      string `json:"createdAt"`
	UpdatedAt      string `json:"updatedAt"`
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
	PricingTiers       []PricingTier       `json:"pricingTiers"`
	Workshops          []Workshop          `json:"workshops"`
	SelfStudySessions  []SelfStudySession  `json:"selfStudySessions"`
	PerformanceReviews []PerformanceReview `json:"performanceReviews"`
	CancelledClasses   []CancelledClass    `json:"cancelledClasses"`
	SessionMoves       []SessionMove       `json:"sessionMoves"`
	SessionOverrides   []SessionOverride   `json:"sessionOverrides"`
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
