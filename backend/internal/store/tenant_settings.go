package store

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"studyhub/internal/core"
	"sync"
	"time"
)

// TenantSettings carries the operational + branding profile for one tenant.
// Cached in-process with a short TTL because these change rarely but are
// read on a high-traffic surface (every PDF, every email, every login).
type TenantSettings struct {
	ID                int    `json:"id"`
	Slug              string `json:"slug"`
	BrandName         string `json:"brandName"`
	BrandTagline      string `json:"brandTagline"`
	LogoPath          string `json:"logoPath"`
	PrimaryColor      string `json:"primaryColor"`
	AddressLine1      string `json:"addressLine1"`
	AddressLine2      string `json:"addressLine2"`
	TaxID             string `json:"taxId"`
	SupportEmail      string `json:"supportEmail"`
	SupportPhone      string `json:"supportPhone"`
	Locale            string `json:"locale"`
	Currency          string `json:"currency"`
	Timezone          string `json:"timezone"`
	InvoiceFooterHTML string `json:"invoiceFooterHtml"`

	// Bank + payment details for the invoice/receipt PDF payment section.
	// Fully configurable per tenant — never hardcoded into the renderer.
	BankName            string `json:"bankName"`
	BankAccountNo       string `json:"bankAccountNo"`
	BankAccountHolder   string `json:"bankAccountHolder"`
	PaymentInstructions string `json:"paymentInstructions"`
	InvoiceTerms        string `json:"invoiceTerms"`
}

type tenantSettingsCacheEntry struct {
	s       *TenantSettings
	expires time.Time
}

var (
	tenantSettingsCacheMu sync.RWMutex
	tenantSettingsCache   = map[int]tenantSettingsCacheEntry{}
)

const tenantSettingsTTL = 10 * time.Minute

// DefaultTenantSettings is the fallback used when a row lookup fails — keeps
// every code path defensive against a DB outage during PDF/email rendering.
var DefaultTenantSettings = &TenantSettings{
	ID:           1,
	Slug:         "default",
	BrandName:    "The Study Hub",
	PrimaryColor: "#C9A227",
	Locale:       "en-MY",
	Currency:     "MYR",
	Timezone:     "Asia/Kuala_Lumpur",
}

// loadTenantSettings returns the settings for tenantID, cached for
// tenantSettingsTTL. Used by template renderers via getBrand() so a typical
// PDF render reads from memory, not the DB.
func LoadTenantSettings(db *DB, tenantID int) *TenantSettings {
	if tenantID == 0 {
		tenantID = 1
	}
	tenantSettingsCacheMu.RLock()
	if e, ok := tenantSettingsCache[tenantID]; ok && time.Now().Before(e.expires) {
		tenantSettingsCacheMu.RUnlock()
		return e.s
	}
	tenantSettingsCacheMu.RUnlock()

	s := &TenantSettings{ID: tenantID}
	err := db.QueryRow(`SELECT
		COALESCE(slug,''), COALESCE(brand_name,'The Study Hub'), COALESCE(brand_tagline,''),
		COALESCE(logo_path,''), COALESCE(primary_color,'#C9A227'),
		COALESCE(address_line1,''), COALESCE(address_line2,''), COALESCE(tax_id,''),
		COALESCE(support_email,''), COALESCE(support_phone,''),
		COALESCE(locale,'en-MY'), COALESCE(currency,'MYR'),
		COALESCE(timezone,'Asia/Kuala_Lumpur'), COALESCE(invoice_footer_html,''),
		COALESCE(bank_name,''), COALESCE(bank_account_no,''), COALESCE(bank_account_holder,''),
		COALESCE(payment_instructions,''), COALESCE(invoice_terms,'')
		FROM tenants WHERE id=?`, tenantID).Scan(
		&s.Slug, &s.BrandName, &s.BrandTagline,
		&s.LogoPath, &s.PrimaryColor,
		&s.AddressLine1, &s.AddressLine2, &s.TaxID,
		&s.SupportEmail, &s.SupportPhone,
		&s.Locale, &s.Currency,
		&s.Timezone, &s.InvoiceFooterHTML,
		&s.BankName, &s.BankAccountNo, &s.BankAccountHolder,
		&s.PaymentInstructions, &s.InvoiceTerms,
	)
	// A generic DB error must NOT be cached — otherwise a transient blip pins
	// the default fallback for the whole TTL. Only cache a real row (err==nil)
	// or a confirmed absence (ErrNoRows). Any other error returns the default
	// without writing the cache, so the next call retries.
	if err != nil && err != sql.ErrNoRows {
		core.Logger.Error("tenant settings load failed", "err", err, "tenant_id", tenantID)
		return DefaultTenantSettings
	}
	if err == sql.ErrNoRows {
		s = DefaultTenantSettings
	}
	tenantSettingsCacheMu.Lock()
	tenantSettingsCache[tenantID] = tenantSettingsCacheEntry{s: s, expires: time.Now().Add(tenantSettingsTTL)}
	tenantSettingsCacheMu.Unlock()
	return s
}

// invalidateTenantSettings drops the cache for one tenant. Called from the
// admin PUT handler so a settings change takes effect on the next read.
func invalidateTenantSettings(tenantID int) {
	tenantSettingsCacheMu.Lock()
	delete(tenantSettingsCache, tenantID)
	tenantSettingsCacheMu.Unlock()
}

// resolveTenantBySlugOrHost returns the tenant id for a public request
// (branding endpoint). Resolution order:
//  1. ?tenant=<slug> query parameter
//  2. Host header subdomain match (e.g. "raffles.studyhub.fit" → slug "raffles")
//  3. Default tenant (id=1)
//
// Used by the branding endpoint and (later) by login when running per-tenant
// subdomains.
func resolveTenantBySlugOrHost(db *DB, r *http.Request) int {
	if slug := strings.TrimSpace(r.URL.Query().Get("tenant")); slug != "" {
		if id := tenantIDForSlug(db, slug); id != 0 {
			return id
		}
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	if i := strings.IndexByte(host, '.'); i > 0 {
		sub := host[:i]
		if sub != "www" {
			if id := tenantIDForSlug(db, sub); id != 0 {
				return id
			}
		}
	}
	return 1
}

func tenantIDForSlug(db *DB, slug string) int {
	var id int
	db.QueryRow(`SELECT id FROM tenants WHERE slug=? AND COALESCE(subscription_status,'active')='active'`, slug).Scan(&id)
	return id
}

// ── HTTP handlers ───────────────────────────────────────────────────────────

// handleBranding is a public endpoint used by the login / register pages
// to fetch tenant-aware branding BEFORE the user has a session. Returns
// just the fields the public shell needs — not the operational stuff
// (tax_id, support_email, etc.).
//
// GET /api/branding[?tenant=<slug>]
func HandleBranding(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tid := resolveTenantBySlugOrHost(db, r)
		s := LoadTenantSettings(db, tid)
		// Cacheable at the edge — branding rarely changes and is the same
		// for every visitor of the same tenant.
		w.Header().Set("Cache-Control", "public, max-age=300")
		core.Respond(w, map[string]any{
			"tenantId":     s.ID,
			"slug":         s.Slug,
			"brandName":    s.BrandName,
			"brandTagline": s.BrandTagline,
			"logoUrl":      brandLogoURL(s),
			"primaryColor": s.PrimaryColor,
			"locale":       s.Locale,
			"currency":     s.Currency,
		})

	}
}

// brandLogoURL turns the stored logo path (which can be "uploads/foo.png"
// from the upload driver or a fully-qualified URL) into the absolute URL
// the browser should fetch.
func brandLogoURL(s *TenantSettings) string {
	if s.LogoPath == "" {
		return ""
	}
	if strings.HasPrefix(s.LogoPath, "http://") || strings.HasPrefix(s.LogoPath, "https://") {
		return s.LogoPath
	}
	return "/api/" + strings.TrimPrefix(s.LogoPath, "/")
}

// handleAdminSettings is the admin-only CRUD endpoint for the current
// tenant's settings row.
//
// GET /api/admin/settings  → full settings struct
// PUT /api/admin/settings  → partial update; fields absent are left alone
func HandleAdminSettings(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", http.StatusForbidden)
			return
		}
		tid := TenantID(c)
		switch r.Method {
		case http.MethodGet:
			core.Respond(w, LoadTenantSettings(db, tid))
		case http.MethodPut:
			var body struct {
				BrandName         *string `json:"brandName"`
				BrandTagline      *string `json:"brandTagline"`
				LogoPath          *string `json:"logoPath"`
				PrimaryColor      *string `json:"primaryColor"`
				AddressLine1      *string `json:"addressLine1"`
				AddressLine2      *string `json:"addressLine2"`
				TaxID             *string `json:"taxId"`
				SupportEmail      *string `json:"supportEmail"`
				SupportPhone      *string `json:"supportPhone"`
				Locale            *string `json:"locale"`
				Currency          *string `json:"currency"`
				Timezone          *string `json:"timezone"`
				InvoiceFooterHTML *string `json:"invoiceFooterHtml"`
				Slug              *string `json:"slug"`

				BankName            *string `json:"bankName"`
				BankAccountNo       *string `json:"bankAccountNo"`
				BankAccountHolder   *string `json:"bankAccountHolder"`
				PaymentInstructions *string `json:"paymentInstructions"`
				InvoiceTerms        *string `json:"invoiceTerms"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				core.RespondError(w, "bad body", 400)
				return
			}
			// Partial-update pattern: each pointer that's non-nil contributes
			// one SET clause. Building the SQL dynamically rather than always
			// updating every column means an admin who only set BrandName
			// doesn't accidentally clear PrimaryColor.
			parts := []string{}
			args := []any{}
			add := func(col string, val *string) {
				if val == nil {
					return
				}
				parts = append(parts, col+"=?")
				args = append(args, *val)
			}
			add("brand_name", body.BrandName)
			add("brand_tagline", body.BrandTagline)
			add("logo_path", body.LogoPath)
			add("primary_color", body.PrimaryColor)
			add("address_line1", body.AddressLine1)
			add("address_line2", body.AddressLine2)
			add("tax_id", body.TaxID)
			add("support_email", body.SupportEmail)
			add("support_phone", body.SupportPhone)
			add("locale", body.Locale)
			add("currency", body.Currency)
			add("timezone", body.Timezone)
			add("invoice_footer_html", body.InvoiceFooterHTML)
			add("slug", body.Slug)
			add("bank_name", body.BankName)
			add("bank_account_no", body.BankAccountNo)
			add("bank_account_holder", body.BankAccountHolder)
			add("payment_instructions", body.PaymentInstructions)
			add("invoice_terms", body.InvoiceTerms)
			if len(parts) == 0 {
				core.Respond(w, LoadTenantSettings(db, tid))
				return
			}
			args = append(args, tid)
			if _, err := db.Exec(`UPDATE tenants SET `+strings.Join(parts, ",")+` WHERE id=?`, args...); err != nil {
				core.RespondError(w, "could not save settings", 500)
				return
			}
			invalidateTenantSettings(tid)
			core.LogAudit(db, tid, c.Email, "tenant_settings_updated", "tenant", "", strings.Join(parts, ","))
			core.Respond(w, LoadTenantSettings(db, tid))
		}
	}
}
