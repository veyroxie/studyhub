package handlers

import (
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"studyhub/internal/core"
	"studyhub/internal/store"
	"time"

	"github.com/go-chi/chi/v5"
)

// ── Payment Proof Upload ──────────────────────────────────────────────────────

// maxProofBytes caps a payment-proof upload. Phone-camera receipts routinely
// exceed 5MB; when MaxBytesReader trips mid-stream the browser surfaces it as a
// connection reset ("network error"), not a clean 400 — so keep it generous
// enough that legitimate receipts never hit it. Kept in sync with
// MAX_PROOF_BYTES in the frontend billing module.
const maxProofBytes = 15 << 20

func HandleUploadProof(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxProofBytes)
		if err := r.ParseMultipartForm(maxProofBytes); err != nil {
			core.RespondError(w, fmt.Sprintf("file too large (max %dMB)", maxProofBytes>>20), 400)
			return
		}
		invoiceID := r.FormValue("invoiceId")
		if invoiceID == "" {
			core.RespondError(w, "invoiceId is required", 400)
			return
		}

		// Verify invoice exists in the caller's tenant and check ownership
		// for parents. All lookups scoped to tenant — a parent guessing a
		// foreign-tenant invoice ID must not be able to read or overwrite it.
		c := core.ClaimsFrom(r)
		if c == nil {
			core.RespondError(w, "auth required", http.StatusUnauthorized)
			return
		}
		// Payment proofs are admin + own-family-parent only. Teachers and other
		// roles fall through the parent-ownership check below, so block them here.
		if c.Role != "parent" && !core.IsAdminRole(c) {
			core.RespondError(w, "forbidden", http.StatusForbidden)
			return
		}
		tw, twArgs := store.ScopeTenant(c, "")
		var studentID string
		invArgs := append([]any{invoiceID}, twArgs...)
		if err := db.QueryRow(`SELECT student_id FROM invoices WHERE id=? AND deleted_at IS NULL`+tw, invArgs...).Scan(&studentID); err != nil {
			core.RespondError(w, "invoice not found", 404)
			return
		}
		if c.Role == "parent" {
			var ownerEmail string
			stuArgs := append([]any{studentID}, twArgs...)
			db.QueryRow(`SELECT contact FROM students WHERE id=?`+tw, stuArgs...).Scan(&ownerEmail)
			if ownerEmail != c.Email {
				core.RespondError(w, "not your invoice", 403)
				return
			}
		}

		file, header, err := r.FormFile("proof")
		if err != nil {
			core.RespondError(w, "proof file is required", 400)
			return
		}
		defer file.Close()

		// Validate file type
		ext := strings.ToLower(filepath.Ext(header.Filename))
		allowed := map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".pdf": true}
		if !allowed[ext] {
			core.RespondError(w, "only jpg, jpeg, png, pdf files are allowed", 400)
			return
		}

		// Validate MIME type by reading file header (magic bytes)
		buf := make([]byte, 512)
		n, _ := file.Read(buf)
		mime := http.DetectContentType(buf[:n])
		file.Seek(0, io.SeekStart) // reset reader position
		allowedMIME := map[string]bool{
			"image/jpeg":      true,
			"image/png":       true,
			"application/pdf": true,
		}
		if !allowedMIME[mime] {
			core.RespondError(w, "file content does not match allowed types (jpg, png, pdf)", 400)
			return
		}

		// Read file into memory (capped by MaxBytesReader above) and
		// hand it to the upload driver. Local disk for single-node; S3 in
		// production multi-pod deploys.
		ts := time.Now().Unix()
		filename := fmt.Sprintf("proof_%s_%d%s", invoiceID, ts, ext)
		buf2, err := io.ReadAll(file)
		if err != nil {
			core.RespondError(w, "could not read upload", 500)
			return
		}
		if err := uploads.Put(filename, mime, buf2); err != nil {
			core.RespondError(w, "could not store file", 500)
			return
		}

		// Update invoice record (already tenant-scoped above)
		proofPath := "uploads/" + filename
		updArgs := append([]any{proofPath, invoiceID}, twArgs...)
		if _, err := db.Exec(`UPDATE invoices SET payment_proof=? WHERE id=?`+tw, updArgs...); err != nil {
			core.RespondError(w, "could not update invoice", 500)
			return
		}

		core.LogAudit(db, store.TenantID(c), c.Email, "proof_uploaded", "invoice", invoiceID, proofPath)

		core.Respond(w, map[string]string{"path": proofPath})
	}
}

func HandleServeUpload(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filename := chi.URLParam(r, "filename")
		// Sanitize: only allow alphanumeric, underscores, dots, dashes
		for _, c := range filename {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '.' || c == '-') {
				core.RespondError(w, "invalid filename", 400)
				return
			}
		}

		// Authorise the caller against the invoice this file belongs to.
		// Filename pattern is "proof_<invoiceID>_<unix>.<ext>" — anything
		// else served from /uploads we treat as inaccessible to non-admins.
		caller := core.ClaimsFrom(r)
		if caller == nil {
			core.RespondError(w, "auth required", http.StatusUnauthorized)
			return
		}
		if caller.Role != "parent" && !core.IsAdminRole(caller) {
			core.RespondError(w, "forbidden", http.StatusForbidden)
			return
		}
		if !strings.HasPrefix(filename, "proof_") {
			core.RespondError(w, "invalid filename", http.StatusBadRequest)
			return
		}
		rest := strings.TrimPrefix(filename, "proof_")
		idx := strings.LastIndex(rest, "_")
		if idx <= 0 {
			core.RespondError(w, "invalid filename", http.StatusBadRequest)
			return
		}
		invoiceID := rest[:idx]
		tw, twArgs := store.ScopeTenant(caller, "")
		var studentID string
		invArgs := append([]any{invoiceID}, twArgs...)
		if err := db.QueryRow(`SELECT student_id FROM invoices WHERE id=? AND deleted_at IS NULL`+tw, invArgs...).Scan(&studentID); err != nil {
			core.RespondError(w, "file not found", http.StatusNotFound)
			return
		}
		if caller.Role == "parent" {
			var ownerEmail string
			stuArgs := append([]any{studentID}, twArgs...)
			db.QueryRow(`SELECT contact FROM students WHERE id=?`+tw, stuArgs...).Scan(&ownerEmail)
			if ownerEmail != caller.Email {
				core.RespondError(w, "not your invoice", http.StatusForbidden)
				return
			}
		}

		// If the driver supports presigned URLs, redirect — saves the API
		// from being a download proxy.
		if redirectURL := uploads.Redirect(filename, 15*time.Minute); redirectURL != "" {
			http.Redirect(w, r, redirectURL, http.StatusTemporaryRedirect)
			return
		}

		rc, err := uploads.Get(filename)
		if err != nil {
			core.RespondError(w, "file not found", 404)
			return
		}
		defer rc.Close()

		ext := strings.ToLower(filepath.Ext(filename))
		switch ext {
		case ".jpg", ".jpeg":
			w.Header().Set("Content-Type", "image/jpeg")
		case ".png":
			w.Header().Set("Content-Type", "image/png")
		case ".pdf":
			w.Header().Set("Content-Type", "application/pdf")
		default:
			w.Header().Set("Content-Type", "application/octet-stream")
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Disposition", "inline; filename="+filename)
		io.Copy(w, rc)
	}
}
