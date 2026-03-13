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
	Allergies       string   `json:"allergies,omitempty"`
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
	ID           string  `json:"id"`
	StudentID    string  `json:"studentId"`
	Description  string  `json:"description"`
	Type         string  `json:"type"`
	Amount       float64 `json:"amount"`
	DueDate      string  `json:"dueDate"`
	Status       string  `json:"status"`
	CreatedOn    string  `json:"createdOn"`
	PaidOn       *string `json:"paidOn"`
	PaymentProof string  `json:"paymentProof,omitempty"`
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
	ID         string   `json:"id"`
	StaffID    string   `json:"staffId"`
	Month      string   `json:"month"`
	BaseSalary float64  `json:"baseSalary"`
	Bonus      float64  `json:"bonus"`
	Deductions float64  `json:"deductions"`
	Total      float64  `json:"total"`
	Status     string   `json:"status"`
	PaidOn     *string  `json:"paidOn"`
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
}
