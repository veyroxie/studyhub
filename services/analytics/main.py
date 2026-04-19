"""
StudyHub Analytics Microservice
================================
A lightweight FastAPI service that handles compute-heavy analytics
and report generation. The Go API calls this via HTTP internally —
never directly exposed to the public internet.

Run locally:  uvicorn main:app --port 8001 --reload
Docker:       handled by docker-compose.yml
"""

from enum import StrEnum

from fastapi import FastAPI, HTTPException
from pydantic import BaseModel
from typing import Any, Optional
from datetime import datetime


app = FastAPI(title="StudyHub Analytics Service", version="1.0.0")


# ── Constants ────────────────────────────────────────────────────────────────

class ReportType(StrEnum):
    Summary = "summary"
    Risk = "risk"
    Detailed = "detailed"


class StudentStatus(StrEnum):
    Active = "Active"
    New = "New"
    Inactive = "Inactive"


class InvoiceStatus(StrEnum):
    Paid = "Paid"
    Unpaid = "Unpaid"
    Overdue = "Overdue"


class AttendanceStatus(StrEnum):
    Present = "Present"
    Late = "Late"
    Absent = "Absent"


ATTENDANCE_RISK_THRESHOLD = 0.7


# ── Request / Response models ─────────────────────────────────────────────────

class Student(BaseModel):
    id: str
    firstName: str
    lastName: str
    status: str
    registeredOn: Optional[str] = None
    enrolledClasses: list[str] = []


class Invoice(BaseModel):
    id: str
    studentId: str
    amount: float
    status: str
    createdOn: Optional[str] = None
    paidOn: Optional[str] = None


class AttendanceRecord(BaseModel):
    id: str
    personId: str
    personType: str
    date: str
    status: str


class ReportRequest(BaseModel):
    tenantId: int
    students: list[Student] = []
    invoices: list[Invoice] = []
    attendance: list[AttendanceRecord] = []
    reportType: str = ReportType.Summary


class ReportResponse(BaseModel):
    tenantId: int
    reportType: str
    generatedAt: str
    data: dict[str, Any]


# ── Health check ──────────────────────────────────────────────────────────────

@app.get("/health")
def health() -> dict[str, str]:
    return {"status": "ok", "service": "analytics"}


# ── Internal report endpoint (called by Go API only) ─────────────────────────

@app.post("/internal/report", response_model=ReportResponse)
def generate_report(req: ReportRequest) -> ReportResponse:
    """
    Called by Go backend via HTTP. Returns computed analytics for a tenant.
    The Go server never exposes this endpoint to the public.
    """
    if req.reportType == ReportType.Summary:
        data = _summary_report(req)
    elif req.reportType == ReportType.Risk:
        data = _risk_report(req)
    elif req.reportType == ReportType.Detailed:
        data = _detailed_report(req)
    else:
        raise HTTPException(status_code=400, detail=f"Unknown reportType: {req.reportType}")

    return ReportResponse(
        tenantId=req.tenantId,
        reportType=req.reportType,
        generatedAt=datetime.utcnow().isoformat() + "Z",
        data=data,
    )


# ── Report builders ───────────────────────────────────────────────────────────

def _summary_report(req: ReportRequest) -> dict[str, int | float]:
    """High-level KPIs: active students, revenue, avg attendance."""
    active = sum(1 for s in req.students if s.status == StudentStatus.Active)
    total_revenue = sum(i.amount for i in req.invoices)
    collected = sum(i.amount for i in req.invoices if i.status == InvoiceStatus.Paid)
    collection_rate = round((collected / total_revenue * 100) if total_revenue > 0 else 0, 1)

    student_att = [a for a in req.attendance if a.personType == "student"]
    present = sum(1 for a in student_att if a.status in (AttendanceStatus.Present, AttendanceStatus.Late))
    att_rate = round((present / len(student_att) * 100) if student_att else 0, 1)

    return {
        "activeStudents": active,
        "totalStudents": len(req.students),
        "totalRevenue": total_revenue,
        "collected": collected,
        "collectionRate": collection_rate,
        "attendanceRate": att_rate,
        "totalSessions": len(student_att),
    }


def _risk_report(req: ReportRequest) -> dict[str, int | list[dict[str, Any]]]:
    """
    Identify at-risk students: low attendance OR overdue invoices.
    Useful for the admin dashboard to flag students needing follow-up.
    """
    at_risk: list[dict[str, Any]] = []

    for s in req.students:
        reasons: list[str] = []

        # Check attendance
        records = [a for a in req.attendance if a.personId == s.id and a.personType == "student"]
        if len(records) >= 3:
            present = sum(1 for a in records if a.status in (AttendanceStatus.Present, AttendanceStatus.Late))
            rate = present / len(records)
            if rate < ATTENDANCE_RISK_THRESHOLD:
                reasons.append(f"Low attendance ({round(rate*100)}%)")

        # Check overdue invoices
        overdue = [i for i in req.invoices if i.studentId == s.id and i.status == InvoiceStatus.Overdue]
        if overdue:
            reasons.append(f"{len(overdue)} overdue invoice(s)")

        if reasons:
            at_risk.append({
                "studentId": s.id,
                "name": f"{s.firstName} {s.lastName}",
                "reasons": reasons,
            })

    return {
        "atRiskCount": len(at_risk),
        "students": at_risk,
    }


def _detailed_report(req: ReportRequest) -> dict[str, list[dict[str, Any]]]:
    """Per-student attendance and billing breakdown."""
    breakdown: list[dict[str, Any]] = []
    for s in req.students:
        records = [a for a in req.attendance if a.personId == s.id and a.personType == "student"]
        present = sum(1 for a in records if a.status in (AttendanceStatus.Present, AttendanceStatus.Late))
        att_rate = round((present / len(records) * 100) if records else 0, 1)

        invoices = [i for i in req.invoices if i.studentId == s.id]
        paid = sum(i.amount for i in invoices if i.status == InvoiceStatus.Paid)
        unpaid = sum(i.amount for i in invoices if i.status != InvoiceStatus.Paid)

        breakdown.append({
            "studentId": s.id,
            "name": f"{s.firstName} {s.lastName}",
            "status": s.status,
            "attendanceRate": att_rate,
            "sessions": len(records),
            "totalPaid": paid,
            "totalUnpaid": unpaid,
        })

    return {"students": breakdown}
