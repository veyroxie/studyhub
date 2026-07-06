package auth

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"studyhub/internal/core"
	"studyhub/internal/store"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var emailRe = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

// ValidateEmail reports whether the address is syntactically valid.
func ValidateEmail(email string) bool {
	return emailRe.MatchString(email)
}

// ValidatePassword enforces the minimum password policy.
func ValidatePassword(password string) (bool, string) {
	if len(password) < 8 {
		return false, "password must be at least 8 characters"
	}
	return true, ""
}

// HashPassword is the canonical entry point for new password storage.
// Delegates to Argon2id so all new accounts use the modern hash. Existing
// bcrypt hashes verify transparently and are upgraded on next login.
func HashPassword(password string) (string, error) {
	return hashPasswordArgon2id(password)
}

// jwtSecret is set from JWT_SECRET env var in main.go — this default is only used in dev
var jwtSecret []byte

// SetJWTSecret installs the process-wide JWT signing secret. Called from main.
func SetJWTSecret(b []byte) { jwtSecret = b }

// JWTSecretLen returns the configured secret length so main can validate it.
func JWTSecretLen() int { return len(jwtSecret) }

func init() {
	core.SetIssueAuthCookie(issueAuthCookie)
}

// Two session lifetimes: a short one for the default browser-session case
// (cookie dies on browser close) and a long one when the user ticks
// "Remember me" on the login form (persistent cookie, 30 days).
const (
	TokenExpiryShort    = 24 * time.Hour      // 1 day — default
	TokenExpiryRemember = 30 * 24 * time.Hour // 30 days — opt-in via "Remember me"
)

// ── Login ─────────────────────────────────────────────────────────────────────

type loginRequest struct {
	Email      string `json:"email"`
	Password   string `json:"password"`
	RememberMe bool   `json:"rememberMe"`
}

// LoginResponse never contains the token — it goes in an HttpOnly cookie instead
type LoginResponse struct {
	Role    string `json:"role"`
	Name    string `json:"name"`
	Email   string `json:"email"`
	StaffID string `json:"staffId,omitempty"`
}

// dummyPasswordHash is a valid argon2id hash verified against on the
// "user not found" login path, so that path spends the same ~Argon2 CPU time
// as a real credential check. Without it, an unknown email returns instantly
// while a known email pays the hashing cost — a timing oracle for enumerating
// which emails have accounts.
var dummyPasswordHash = func() string {
	h, err := HashPassword("timing-equalizer-not-a-real-password")
	if err != nil {
		return ""
	}
	return h
}()

func HandleLogin(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req loginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			core.RespondError(w, "invalid request body", http.StatusBadRequest)
			return
		}
		req.Email = strings.ToLower(strings.TrimSpace(req.Email))

		if !ValidateEmail(req.Email) {
			core.RespondError(w, "invalid email format", http.StatusBadRequest)
			return
		}
		if len(req.Password) == 0 {
			core.RespondError(w, "password is required", http.StatusBadRequest)
			return
		}

		var (
			id          int
			hash        string
			role        string
			name        string
			tenantID    int
			status      string
			failedCount int
			lockedUntil sql.NullTime
		)
		err := db.QueryRow(
			`SELECT id, password_hash, role, name, tenant_id, COALESCE(status,'active'), COALESCE(failed_login_count,0), locked_until FROM users WHERE email = ?`, req.Email,
		).Scan(&id, &hash, &role, &name, &tenantID, &status, &failedCount, &lockedUntil)

		if err == sql.ErrNoRows {
			// Burn the same Argon2 time a real password check would, so an
			// unknown email is indistinguishable from a wrong password by
			// response latency. Same generic error either way.
			_ = VerifyPassword(dummyPasswordHash, req.Password)
			core.RespondError(w, "invalid credentials", http.StatusUnauthorized)
			return
		}
		if err != nil {
			core.RespondError(w, "server error", http.StatusInternalServerError)
			return
		}

		// ── Account lockout gate ───────────────────────────────────────────
		// If the account is currently locked, reject before even checking the
		// password. Use a generic message that includes how many minutes are
		// left so a confused legitimate user knows when to try again.
		if lockedUntil.Valid && lockedUntil.Time.After(time.Now()) {
			minsLeft := int(time.Until(lockedUntil.Time).Minutes()) + 1
			core.RespondError(w, fmt.Sprintf("account temporarily locked due to too many failed attempts — try again in %d minutes", minsLeft), http.StatusForbidden)
			return
		}

		if err := VerifyPassword(hash, req.Password); err != nil {
			// Atomic increment + lock — UPDATE … RETURNING reads the new
			// counter under the row lock, so concurrent failed attempts
			// can't all see the same pre-increment value and bypass the
			// lockout threshold.
			const maxAttempts = 5
			const lockDuration = 15 * time.Minute
			var newCount int
			if err := db.QueryRow(
				`UPDATE users
				    SET failed_login_count = COALESCE(failed_login_count,0) + 1,
				        locked_until = CASE WHEN COALESCE(failed_login_count,0) + 1 >= ? THEN ? ELSE locked_until END
				  WHERE id = ?
				RETURNING failed_login_count`,
				maxAttempts, time.Now().Add(lockDuration), id,
			).Scan(&newCount); err != nil {
				core.LogFromReq(r).Error("failed to update login failure count", "err", err, "user_id", id)
			}
			if newCount >= maxAttempts {
				core.LogFromReq(r).Warn("account locked after failed logins", "email", req.Email, "user_id", id, "attempts", newCount)
				core.RespondError(w, fmt.Sprintf("account locked after %d failed attempts — try again in %d minutes", maxAttempts, int(lockDuration.Minutes())), http.StatusForbidden)
				return
			}
			core.RespondError(w, "invalid credentials", http.StatusUnauthorized)
			return
		}

		// Successful password check — reset the failure counter so a parent
		// who got their password wrong twice doesn't carry that history
		// forever.
		if failedCount > 0 || lockedUntil.Valid {
			if _, err := db.Exec(`UPDATE users SET failed_login_count=0, locked_until=NULL WHERE id=?`, id); err != nil {
				core.LogFromReq(r).Error("failed to reset login failure count", "err", err, "user_id", id)
			}
		}

		// Opportunistic hash upgrade: rehash legacy bcrypt passwords to
		// Argon2id on next login. Failure is logged but doesn't block the
		// login — the user can still authenticate, we'll just retry next time.
		if hashShouldRehash(hash) {
			if newHash, err := HashPassword(req.Password); err == nil {
				if _, err := db.Exec(`UPDATE users SET password_hash=? WHERE id=?`, newHash, id); err != nil {
					core.LogFromReq(r).Error("password rehash failed", "err", err, "user_id", id)
				}
			}
		}

		// Block login until the email has been verified. Frontend looks for
		// the "needs_verification" sentinel in the error message and shows a
		// "Resend verification email" link.
		if status == "pending_verification" {
			core.RespondError(w, "needs_verification: please verify your email — check your inbox for the link we sent", http.StatusForbidden)
			return
		}

		// MFA gate: if the user has MFA enabled, defer the cookie issue
		// to /api/auth/mfa/verify. Hand the client a single-use 5-minute
		// intermediate token they POST back with their TOTP code.
		var mfaEnabled bool
		db.QueryRow(`SELECT COALESCE(mfa_enabled,false) FROM users WHERE id=?`, id).Scan(&mfaEnabled)
		if mfaEnabled {
			interim, err := issueMFAIntermediate(db, id, tenantID, req.Email, role, name, req.RememberMe)
			if err != nil {
				core.RespondError(w, "could not start MFA challenge", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{
				"mfaRequired": true,
				"token":       interim,
			})
			return
		}

		issueAuthCookie(w, r, id, tenantID, req.Email, role, name, req.RememberMe)
		// Issue a refresh token alongside the access cookie. The frontend's
		// silent-refresh interceptor uses it to rotate access JWTs without
		// re-prompting for credentials.
		if err := store.IssueRefreshToken(db, w, r, id, tenantID, ""); err != nil {
			core.LogFromReq(r).Error("issue refresh token failed", "err", err, "user_id", id)
		}

		// Return role/name/email — NOT the token itself.
		// For teachers, also look up their staff ID so the frontend can
		// populate App.currentTeacher and render the teacher dashboard.
		base := LoginResponse{Role: role, Name: name, Email: req.Email}
		if role == "teacher" {
			// Look up the staff row in the same tenant as the user we
			// authenticated — staff.email is not globally unique, so a
			// cross-tenant lookup could return the wrong staffId for a
			// shared teacher email.
			db.QueryRow(`SELECT id FROM staff WHERE email=? AND tenant_id=? AND deleted_at IS NULL LIMIT 1`, req.Email, tenantID).Scan(&base.StaffID)
		}
		// Surface the ToS gate in the login payload so the frontend can
		// block app entry on first-time login without an extra /me call.
		var tosV int
		db.QueryRow(`SELECT COALESCE(tos_accepted_version,0) FROM users WHERE id=?`, id).Scan(&tosV)
		resp := struct {
			LoginResponse
			MustAcceptToS bool `json:"mustAcceptTos"`
		}{
			LoginResponse: base,
			MustAcceptToS: tosV < core.CurrentToSVersion,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

// issueAuthCookie writes the sh_token HttpOnly cookie + sets up the session.
// Extracted so both the password-only path and the MFA-completed path use the
// same logic.
func issueAuthCookie(w http.ResponseWriter, r *http.Request, userID, tenantID int, email, role, name string, rememberMe bool) {
	expiry := TokenExpiryShort
	if rememberMe {
		expiry = TokenExpiryRemember
	}
	token, err := MakeToken(userID, tenantID, email, role, name, expiry)
	if err != nil {
		http.Error(w, "could not sign token", http.StatusInternalServerError)
		return
	}
	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	cookie := &http.Cookie{
		Name:     "sh_token",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
	if rememberMe {
		cookie.Expires = time.Now().Add(expiry)
	}
	http.SetCookie(w, cookie)
}

// handleLogout clears the auth cookie AND revokes the token server-side
// so a stolen cookie can't be replayed for the remaining JWT lifetime.
//
// Mounted outside the JWT middleware so callers with an already-expired
// or malformed cookie can still log out cleanly — that means claimsFrom
// returns nil here. We re-parse the cookie inline to extract the jti.
// The revocation row expires when the JWT would have expired anyway;
// the background cleanup job sweeps stale rows.
func HandleLogout(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var tokenStr string
		if cookie, err := r.Cookie("sh_token"); err == nil {
			tokenStr = cookie.Value
		}
		if tokenStr == "" {
			if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
				tokenStr = strings.TrimPrefix(auth, "Bearer ")
			}
		}
		if tokenStr != "" {
			claims := &core.Claims{}
			// Parse without enforcing expiry — we want to revoke even
			// almost-expired tokens (the cleanup job will drop them shortly).
			parser := jwt.NewParser(jwt.WithoutClaimsValidation())
			if _, err := parser.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
				return jwtSecret, nil
			}); err == nil && claims.ID != "" {
				exp := time.Now().Add(TokenExpiryRemember)
				if claims.ExpiresAt != nil {
					exp = claims.ExpiresAt.Time
				}
				revokeToken(db, claims.ID, claims.UserID, exp, "logout")
			}
		}
		// Revoke the refresh-token family and clear its cookie too — otherwise
		// the sh_refresh cookie (7-day TTL, scoped to /api/auth) stays valid
		// and can mint a fresh access session after "logout".
		store.RevokeRefreshByCookie(db, r)
		store.ClearRefreshCookie(w, r)
		secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
		http.SetCookie(w, &http.Cookie{
			Name:     "sh_token",
			Value:    "",
			Path:     "/",
			Expires:  time.Unix(0, 0),
			HttpOnly: true,
			Secure:   secure,
			SameSite: http.SameSiteLaxMode,
		})
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleMe returns the current user's info from their cookie.
// For teachers, also looks up staffId so the frontend can restore
// App.currentTeacher on page refresh.
func HandleMe(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if c == nil {
			core.RespondError(w, "not authenticated", http.StatusUnauthorized)
			return
		}
		// Re-validate the session against the live DB row, not the JWT
		// claim. This catches: name changes (reflect immediately), role
		// changes (escalate/de-escalate without re-login), and account
		// status flips (suspended/disabled users lose access on the next
		// request rather than waiting for token expiry).
		var dbName, dbRole, dbStatus string
		err := db.QueryRow(
			`SELECT COALESCE(name,''), COALESCE(role,''), COALESCE(status,'active') FROM users WHERE email=?`,
			c.Email,
		).Scan(&dbName, &dbRole, &dbStatus)
		if err != nil {
			core.RespondError(w, "account no longer exists", http.StatusUnauthorized)
			return
		}
		if dbStatus != "active" {
			core.RespondError(w, "account is "+dbStatus, http.StatusForbidden)
			return
		}
		if dbName != "" {
			c.Name = dbName
		}
		if dbRole != "" {
			c.Role = dbRole
		}
		// Surface ToS gate in the /me payload so the frontend boot routine
		// can route to the accept page before showing the dashboard.
		var tosV int
		db.QueryRow(`SELECT COALESCE(tos_accepted_version,0) FROM users WHERE email=?`, c.Email).Scan(&tosV)
		resp := struct {
			LoginResponse
			MustAcceptToS bool `json:"mustAcceptTos"`
		}{
			LoginResponse: LoginResponse{Role: c.Role, Name: c.Name, Email: c.Email},
			MustAcceptToS: tosV < core.CurrentToSVersion,
		}
		if c.Role == "teacher" {
			tw, twArgs := store.ScopeTenant(c, "")
			staffArgs := append([]any{c.Email}, twArgs...)
			db.QueryRow(`SELECT id FROM staff WHERE email=? AND deleted_at IS NULL`+tw+` LIMIT 1`, staffArgs...).Scan(&resp.StaffID)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}
}

func MakeToken(userID, tenantID int, email, role, name string, expiry time.Duration) (string, error) {
	jti, err := store.GenerateToken()
	if err != nil {
		return "", err
	}
	claims := core.Claims{
		UserID:   userID,
		TenantID: tenantID,
		Email:    email,
		Role:     role,
		Name:     name,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti, // 'jti' — opaque token id used by the revocation table
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(expiry)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(jwtSecret)
}

// revokeToken records a JWT ID in the revoked_tokens table so the JWT
// middleware rejects it on subsequent requests. Idempotent — re-revoking
// is a no-op via ON CONFLICT.
//
// Critically also drops the in-memory cache entry. The request that
// triggered the revocation just populated the cache with revoked=false
// (the row didn't exist yet when the middleware checked); without this
// purge the next request within revokedTokenTTL would skip the DB read
// and let the just-killed token through.
func revokeToken(db *store.DB, jti string, userID int, expiresAt time.Time, reason string) {
	if jti == "" {
		return
	}
	db.Exec(
		`INSERT INTO revoked_tokens(jti, user_id, expires_at, reason) VALUES(?,?,?,?) ON CONFLICT(jti) DO NOTHING`,
		jti, userID, expiresAt, reason,
	)
	revokedTokenCache.Delete(jti)
}

// ── JWT Middleware ─────────────────────────────────────────────────────────────
// Reads the token from the HttpOnly cookie (not Authorization header).
// Falls back to Bearer header for API clients / testing.

// userStatusCache lazily caches users.status by user id with a short TTL so
// the jwt middleware doesn't hit Postgres on every request. Cache misses
// query the DB; status changes (suspend/reactivate) propagate within
// userStatusTTL. Keyed by user id because email is mutable.
var userStatusCache sync.Map // map[int]userStatusEntry

type userStatusEntry struct {
	status string
	expiry time.Time
}

const userStatusTTL = 30 * time.Second

func lookupUserStatus(db *store.DB, userID int) string {
	if v, ok := userStatusCache.Load(userID); ok {
		e := v.(userStatusEntry)
		if time.Now().Before(e.expiry) {
			return e.status
		}
	}
	var status string
	db.QueryRow(`SELECT COALESCE(status,'active') FROM users WHERE id=?`, userID).Scan(&status)
	if status == "" {
		status = "unknown"
	}
	userStatusCache.Store(userID, userStatusEntry{status: status, expiry: time.Now().Add(userStatusTTL)})
	return status
}

// invalidateUserStatusCache drops the cached status so the next request
// re-reads from the DB. Call this when admin suspends/reactivates a user.
func InvalidateUserStatusCache(userID int) {
	userStatusCache.Delete(userID)
}

// revokedTokenCache shadows the DB so the middleware doesn't query Postgres
// on every request. Same TTL as userStatusCache. A revoked JWT takes effect
// within revokedTokenTTL across the cluster — acceptable for a logout
// signal since the JWT itself has hours-to-days of validity.
var revokedTokenCache sync.Map // map[string]time.Time — jti → expiry instant

const revokedTokenTTL = 30 * time.Second

// isTokenRevoked checks the cache then the DB. Returns true when the jti
// has a row in revoked_tokens that's still within its original expiry
// window. Empty jti (legacy tokens issued before this commit) → not
// revoked.
func isTokenRevoked(db *store.DB, jti string) bool {
	if jti == "" {
		return false
	}
	type cacheEntry struct {
		revoked   bool
		checkedAt time.Time
	}
	if v, ok := revokedTokenCache.Load(jti); ok {
		e := v.(cacheEntry)
		if time.Since(e.checkedAt) < revokedTokenTTL {
			return e.revoked
		}
	}
	var cnt int
	db.QueryRow(`SELECT COUNT(*) FROM revoked_tokens WHERE jti=? AND expires_at > NOW()`, jti).Scan(&cnt)
	rev := cnt > 0
	revokedTokenCache.Store(jti, cacheEntry{revoked: rev, checkedAt: time.Now()})
	return rev
}

// jwtMiddleware authenticates the request and enforces account status.
// A suspended/disabled user with a still-valid JWT is rejected here so
// suspension takes effect within userStatusTTL (not at JWT expiry).
func JWTMiddleware(db *store.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenStr := ""

			// 1. Try HttpOnly cookie first (browser clients)
			if cookie, err := r.Cookie("sh_token"); err == nil {
				tokenStr = cookie.Value
			}

			// 2. Fall back to Authorization header (API clients, tests)
			if tokenStr == "" {
				if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
					tokenStr = strings.TrimPrefix(auth, "Bearer ")
				}
			}

			if tokenStr == "" {
				core.RespondError(w, "missing token", http.StatusUnauthorized)
				return
			}

			claims := &core.Claims{}
			token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
				if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, jwt.ErrSignatureInvalid
				}
				return jwtSecret, nil
			})
			if err != nil || !token.Valid {
				// Clear the bad cookie
				http.SetCookie(w, &http.Cookie{Name: "sh_token", Value: "", Path: "/", Expires: time.Unix(0, 0), HttpOnly: true})
				core.RespondError(w, "invalid or expired token", http.StatusUnauthorized)
				return
			}

			if claims.UserID > 0 {
				if s := lookupUserStatus(db, claims.UserID); s != "active" {
					core.RespondError(w, "account is "+s, http.StatusForbidden)
					return
				}
			}
			// Revocation check — logout / password change / admin force-out
			// records the JWT id here. Cached for 30s to keep this off the
			// hot path; a logged-out token is rejected within that window
			// even though the JWT itself remains cryptographically valid.
			if claims.ID != "" && isTokenRevoked(db, claims.ID) {
				http.SetCookie(w, &http.Cookie{Name: "sh_token", Value: "", Path: "/", Expires: time.Unix(0, 0), HttpOnly: true})
				core.RespondError(w, "session ended — please sign in again", http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, core.WithClaims(r, claims))
		})
	}
}

func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := core.ClaimsFrom(r)
		if !core.IsAdminRole(c) {
			core.RespondError(w, "admin only", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// JWTSecret returns the configured signing secret. Used by callers outside the
// auth package that must validate or derive from the same key (the WS upgrade
// path and the iCal feed HMAC).
func JWTSecret() []byte { return jwtSecret }
