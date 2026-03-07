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
}

type Staff struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	FullName string  `json:"fullName"`
	Role     string  `json:"role"`
	Email    string  `json:"email"`
	Phone    string  `json:"phone"`
	Salary   float64 `json:"salary"`
	JoinDate string  `json:"joinDate"`
	Status   string  `json:"status"`
}

type Invoice struct {
	ID          string  `json:"id"`
	StudentID   string  `json:"studentId"`
	Description string  `json:"description"`
	Type        string  `json:"type"`
	Amount      float64 `json:"amount"`
	DueDate     string  `json:"dueDate"`
	Status      string  `json:"status"`
	CreatedOn   string  `json:"createdOn"`
	PaidOn      *string `json:"paidOn"`
}

type Announcement struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Message   string `json:"message"`
	Audience  string `json:"audience"`
	Type      string `json:"type"`
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
	ID               string `json:"id"`
	ParentName       string `json:"parentName"`
	Email            string `json:"email"`
	Phone            string `json:"phone"`
	EmergencyName    string `json:"emergencyName"`
	EmergencyPhone   string `json:"emergencyPhone"`
	StudentFirstName string `json:"studentFirstName"`
	StudentLastName  string `json:"studentLastName"`
	StudentDOB       string `json:"studentDob"`
	StudentGender    string `json:"studentGender"`
	ClassInterest    string `json:"classInterest"`
	Notes            string `json:"notes"`
	SubmittedOn      string `json:"submittedOn"`
	Status           string `json:"status"` // pending | approved | rejected
}

// Snapshot is what GET /api/snapshot returns — identical shape to App.DATA
type Snapshot struct {
	Students      []Student      `json:"students"`
	Classes       []Class        `json:"classes"`
	Staff         []Staff        `json:"staff"`
	Invoices      []Invoice      `json:"invoices"`
	Announcements []Announcement `json:"announcements"`
	Attendance    []Attendance   `json:"attendance"`
	Payroll       []Payroll      `json:"payroll"`
	Registrations []Registration `json:"registrations,omitempty"`
}
