package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func jsonDecode(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

// MFA (TOTP RFC 6238) for admin and superadmin accounts.
//
// Storage: users.mfa_secret holds the base32 TOTP secret (set when enrolled);
//          users.mfa_enabled flips to TRUE once the user confirms the first
//          code; users.mfa_recovery_codes is a JSON array of one-time
//          recovery codes (hashed) the user can use if they lose their
//          authenticator.
//
// Flow:
//   1. Admin hits POST /api/auth/mfa/setup — server generates a secret, returns
//      otpauth:// URI for QR code rendering. mfa_enabled stays FALSE.
//   2. Admin scans, enters a 6-digit code, POSTs /api/auth/mfa/confirm.
//      On success: mfa_enabled=TRUE, recovery codes returned ONCE.
//   3. On subsequent logins, if mfa_enabled, the password check returns
//      `mfa_required: true` with a short-lived intermediate token. The
//      client then POSTs the TOTP code to /api/auth/mfa/verify which
//      issues the real session cookie.
//
// Self-contained — no external TOTP library.

const totpDigits = 6
const totpInterval int64 = 30
const totpDriftSteps = 1 // accept ±30s drift

// generateTOTPSecret returns a fresh base32-encoded secret (160 bits).
func generateTOTPSecret() (string, error) {
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw), nil
}

// totpCode computes the RFC-6238 code for a given base32 secret and time.
func totpCode(secretB32 string, t time.Time) (string, error) {
	secretB32 = strings.ToUpper(strings.TrimSpace(secretB32))
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secretB32)
	if err != nil {
		return "", err
	}
	counter := t.Unix() / totpInterval
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, uint64(counter))
	mac := hmac.New(sha1.New, secret)
	mac.Write(buf)
	h := mac.Sum(nil)
	offset := h[len(h)-1] & 0x0F
	bin := (uint32(h[offset]&0x7F) << 24) |
		(uint32(h[offset+1]) << 16) |
		(uint32(h[offset+2]) << 8) |
		uint32(h[offset+3])
	code := bin % uint32Pow10(totpDigits)
	return fmt.Sprintf("%0*d", totpDigits, code), nil
}

func uint32Pow10(n int) uint32 {
	out := uint32(1)
	for i := 0; i < n; i++ {
		out *= 10
	}
	return out
}

// verifyTOTPCode allows ±totpDriftSteps to tolerate clock skew.
func verifyTOTPCode(secret, code string) bool {
	if len(code) != totpDigits {
		return false
	}
	now := time.Now()
	for d := -totpDriftSteps; d <= totpDriftSteps; d++ {
		t := now.Add(time.Duration(d) * time.Duration(totpInterval) * time.Second)
		if got, err := totpCode(secret, t); err == nil && got == code {
			return true
		}
	}
	return false
}

// otpauthURI builds the standard otpauth:// URI a QR-code library can encode.
// Example: otpauth://totp/StudyHub:admin@x.com?secret=ABC&issuer=StudyHub
func otpauthURI(issuer, accountName, secret string) string {
	u := &url.URL{
		Scheme: "otpauth",
		Host:   "totp",
		Path:   "/" + issuer + ":" + accountName,
	}
	q := u.Query()
	q.Set("secret", secret)
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", "6")
	q.Set("period", "30")
	u.RawQuery = q.Encode()
	return u.String()
}

// generateRecoveryCodes returns 10 single-use recovery codes — 8 hex chars
// each (32 bits of entropy, formatted with a dash for readability).
func generateRecoveryCodes() ([]string, error) {
	codes := make([]string, 10)
	for i := range codes {
		b := make([]byte, 4)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		codes[i] = fmt.Sprintf("%02x%02x-%02x%02x", b[0], b[1], b[2], b[3])
	}
	return codes, nil
}

// ── HTTP handlers ────────────────────────────────────────────────────────────

func handleMFASetup(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if !isAdminRole(c) {
			respondError(w, "admin only", http.StatusForbidden)
			return
		}
		var enabled bool
		db.QueryRow(`SELECT COALESCE(mfa_enabled,false) FROM users WHERE id=?`, c.UserID).Scan(&enabled)
		if enabled {
			respondError(w, "MFA already enabled — disable first to re-enrol", http.StatusConflict)
			return
		}
		secret, err := generateTOTPSecret()
		if err != nil {
			respondError(w, "server error", 500)
			return
		}
		// Store unconfirmed secret. mfa_enabled stays false until /confirm.
		if _, err := db.Exec(`UPDATE users SET mfa_secret=? WHERE id=?`, secret, c.UserID); err != nil {
			respondError(w, "could not store secret", 500)
			return
		}
		uri := otpauthURI("StudyHub", c.Email, secret)
		respond(w, map[string]string{"secret": secret, "uri": uri})
	}
}

func handleMFAConfirm(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if !isAdminRole(c) {
			respondError(w, "admin only", http.StatusForbidden)
			return
		}
		var body struct {
			Code string `json:"code"`
		}
		if err := jsonDecode(r, &body); err != nil {
			respondError(w, "bad body", 400)
			return
		}
		var secret string
		var enabled bool
		db.QueryRow(`SELECT COALESCE(mfa_secret,''), COALESCE(mfa_enabled,false) FROM users WHERE id=?`, c.UserID).Scan(&secret, &enabled)
		if secret == "" {
			respondError(w, "no MFA setup in progress — start with POST /api/auth/mfa/setup", http.StatusBadRequest)
			return
		}
		if enabled {
			respondError(w, "MFA already confirmed", http.StatusConflict)
			return
		}
		if !verifyTOTPCode(secret, body.Code) {
			respondError(w, "code did not match — check your authenticator clock", http.StatusBadRequest)
			return
		}
		codes, err := generateRecoveryCodes()
		if err != nil {
			respondError(w, "server error", 500)
			return
		}
		// Hash recovery codes before storing — they're one-time-use credentials.
		hashed := make([]string, len(codes))
		for i, c := range codes {
			h, _ := hashPasswordArgon2id(c)
			hashed[i] = h
		}
		codesJSON := jsonArr(hashed)
		if _, err := db.Exec(`UPDATE users SET mfa_enabled=true, mfa_recovery_codes=? WHERE id=?`, codesJSON, c.UserID); err != nil {
			respondError(w, "could not enable MFA", 500)
			return
		}
		logAudit(db, c.Email, "mfa_enabled", "user", fmt.Sprintf("%d", c.UserID), "")
		respond(w, map[string]any{
			"enabled":       true,
			"recoveryCodes": codes,
			"notice":        "Save these recovery codes now — they will not be shown again.",
		})
	}
}

func handleMFADisable(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := claimsFrom(r)
		if !isAdminRole(c) {
			respondError(w, "admin only", http.StatusForbidden)
			return
		}
		if _, err := db.Exec(`UPDATE users SET mfa_enabled=false, mfa_secret='', mfa_recovery_codes='[]' WHERE id=?`, c.UserID); err != nil {
			respondError(w, "could not disable MFA", 500)
			return
		}
		logAudit(db, c.Email, "mfa_disabled", "user", fmt.Sprintf("%d", c.UserID), "")
		respond(w, map[string]bool{"enabled": false})
	}
}

// handleMFAVerify is the second step of login when MFA is enabled. The
// client received an mfa_required response from /api/auth/login along with
// a short-lived intermediate token; they POST that token + their TOTP code
// here to get the real auth cookie.
func handleMFAVerify(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Token        string `json:"token"`
			Code         string `json:"code"`
			RecoveryCode string `json:"recoveryCode"`
		}
		if err := jsonDecode(r, &body); err != nil {
			respondError(w, "bad body", 400)
			return
		}
		t, err := consumeMFAIntermediate(db, body.Token)
		if err != nil {
			respondError(w, "intermediate token is invalid or expired — start login again", http.StatusUnauthorized)
			return
		}
		var secret, recoveryJSON string
		db.QueryRow(`SELECT COALESCE(mfa_secret,''), COALESCE(mfa_recovery_codes,'[]') FROM users WHERE id=?`, t.userID).Scan(&secret, &recoveryJSON)
		ok := false
		if body.Code != "" {
			ok = verifyTOTPCode(secret, body.Code)
		} else if body.RecoveryCode != "" {
			ok = consumeRecoveryCode(db, t.userID, body.RecoveryCode, recoveryJSON)
		}
		if !ok {
			respondError(w, "code did not match", http.StatusUnauthorized)
			return
		}
		// Issue the real auth cookie now.
		issueAuthCookie(w, r, t.userID, t.tenantID, t.email, t.role, t.name, t.rememberMe)
		respond(w, loginResponse{Role: t.role, Name: t.name, Email: t.email})
	}
}

// consumeRecoveryCode finds and atomically removes a matching recovery code
// from the user's stored list. Returns true on success.
func consumeRecoveryCode(db *DB, userID int, supplied, recoveryJSON string) bool {
	hashes := parseArr(recoveryJSON)
	remaining := make([]string, 0, len(hashes))
	matched := false
	for _, h := range hashes {
		if !matched && verifyArgon2id(supplied, h) == nil {
			matched = true
			continue
		}
		remaining = append(remaining, h)
	}
	if !matched {
		return false
	}
	db.Exec(`UPDATE users SET mfa_recovery_codes=? WHERE id=?`, jsonArr(remaining), userID)
	return true
}

// ── MFA intermediate tokens ─────────────────────────────────────────────────
//
// Stored in mfa_intermediate_tokens table. 5-minute TTL. Single-use.

type mfaIntermediate struct {
	userID     int
	tenantID   int
	email      string
	role       string
	name       string
	rememberMe bool
}

func issueMFAIntermediate(db *DB, userID, tenantID int, email, role, name string, rememberMe bool) (string, error) {
	token, err := generateToken()
	if err != nil {
		return "", err
	}
	_, err = db.Exec(`INSERT INTO mfa_intermediate(token,user_id,tenant_id,email,role,name,remember_me,expires_at) VALUES(?,?,?,?,?,?,?,?)`,
		token, userID, tenantID, email, role, name, rememberMe, time.Now().Add(5*time.Minute))
	if err != nil {
		return "", err
	}
	return token, nil
}

func consumeMFAIntermediate(db *DB, token string) (*mfaIntermediate, error) {
	if token == "" {
		return nil, errors.New("missing token")
	}
	var m mfaIntermediate
	var expiresAt time.Time
	err := db.QueryRow(
		`DELETE FROM mfa_intermediate WHERE token=? AND expires_at > NOW() RETURNING user_id, tenant_id, email, role, name, remember_me, expires_at`,
		token,
	).Scan(&m.userID, &m.tenantID, &m.email, &m.role, &m.name, &m.rememberMe, &expiresAt)
	if err != nil {
		return nil, err
	}
	return &m, nil
}
