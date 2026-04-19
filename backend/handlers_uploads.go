package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// ── Payment Proof Upload ──────────────────────────────────────────────────────

func handleUploadProof(db *DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Limit request body to 5MB
		r.Body = http.MaxBytesReader(w, r.Body, 5<<20)
		if err := r.ParseMultipartForm(5 << 20); err != nil {
			respondError(w, "file too large (max 5MB)", 400)
			return
		}
		invoiceID := r.FormValue("invoiceId")
		if invoiceID == "" {
			respondError(w, "invoiceId is required", 400)
			return
		}

		// Verify invoice exists and check ownership for parents
		c := claimsFrom(r)
		var studentID string
		if err := db.QueryRow(`SELECT student_id FROM invoices WHERE id=? AND deleted_at IS NULL`, invoiceID).Scan(&studentID); err != nil {
			respondError(w, "invoice not found", 404)
			return
		}
		if c != nil && c.Role == "parent" {
			var ownerEmail string
			db.QueryRow(`SELECT contact FROM students WHERE id=?`, studentID).Scan(&ownerEmail)
			if ownerEmail != c.Email {
				respondError(w, "not your invoice", 403)
				return
			}
		}

		file, header, err := r.FormFile("proof")
		if err != nil {
			respondError(w, "proof file is required", 400)
			return
		}
		defer file.Close()

		// Validate file type
		ext := strings.ToLower(filepath.Ext(header.Filename))
		allowed := map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".pdf": true}
		if !allowed[ext] {
			respondError(w, "only jpg, jpeg, png, pdf files are allowed", 400)
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
			respondError(w, "file content does not match allowed types (jpg, png, pdf)", 400)
			return
		}

		// Create uploads directory
		if err := os.MkdirAll("uploads", 0755); err != nil {
			respondError(w, "could not create uploads directory", 500)
			return
		}

		// Save file
		ts := time.Now().Unix()
		filename := fmt.Sprintf("proof_%s_%d%s", invoiceID, ts, ext)
		savePath := filepath.Join("uploads", filename)
		dst, err := os.Create(savePath)
		if err != nil {
			respondError(w, "could not save file", 500)
			return
		}
		defer dst.Close()
		if _, err := io.Copy(dst, file); err != nil {
			respondError(w, "could not write file", 500)
			return
		}

		// Update invoice record
		proofPath := "uploads/" + filename
		tid := tenantID(c)
		if _, err := db.Exec(`UPDATE invoices SET payment_proof=? WHERE id=? AND (tenant_id=? OR ?=0)`, proofPath, invoiceID, tid, tid); err != nil {
			respondError(w, "could not update invoice", 500)
			return
		}

		// Audit log
		if c := claimsFrom(r); c != nil {
			logAudit(db, c.Email, "proof_uploaded", "invoice", invoiceID, proofPath)
		}

		respond(w, map[string]string{"path": proofPath})
	}
}

func handleServeUpload() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		filename := chi.URLParam(r, "filename")
		// Sanitize: only allow alphanumeric, underscores, dots, dashes
		for _, c := range filename {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '.' || c == '-') {
				respondError(w, "invalid filename", 400)
				return
			}
		}
		filePath := filepath.Join("uploads", filename)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			respondError(w, "file not found", 404)
			return
		}

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
		http.ServeFile(w, r, filePath)
	}
}
