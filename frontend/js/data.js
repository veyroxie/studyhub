window.App = window.App || {};

// App.DATA declares the SHAPE of the client-side store. It must stay empty.
//
// This file used to hold a "demo" dataset. It was not demo data: all twelve
// students were real children, with real names and real dates of birth, and
// eight of them had since had their names erased from the database while their
// details remained here. This file is served without authentication at
// /js/data.js, so anyone could download it.
//
// It reached users because App.Store fell back to this dataset whenever the
// browser's cached copy was missing — which happens on every session expiry,
// since a 401 deliberately clears local storage. Staff saw records for
// children who are not enrolled and invoices that do not exist, which reads
// exactly like catastrophic data loss.
//
// Nothing may be added here. Real data belongs in the database and reaches the
// browser only through an authenticated snapshot. Invented data is no better:
// the failure was showing ANY fabricated record to someone who believed it.
// Locked by frontend/tests/unit/data-shape.test.mjs.
App.DATA = {
  students: [],
  classes: [],
  staff: [],
  invoices: [],
  announcements: [],
  attendance: [],
  payroll: [],
  messages: [],
  cancelledClasses: [],
  holidays: [],
  feedback: [],
  pricingTiers: [],
  workshops: [],
  performanceReviews: [],
  selfStudySessions: []
};
