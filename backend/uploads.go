package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// uploadStore abstracts where payment proofs live. Two impls:
//
//   - localStore writes to a host-mounted ./uploads directory. Default.
//     Fine for a single-node deployment.
//   - s3Store writes to an S3-compatible bucket (AWS S3, DO Spaces,
//     Backblaze B2, MinIO). Required for multi-pod deployments because
//     local disk doesn't survive a pod restart.
//
// Selection is driven by env:
//   UPLOAD_STORE=local              (default)
//   UPLOAD_STORE=s3 + S3_ENDPOINT + S3_BUCKET + S3_REGION
//                    + S3_ACCESS_KEY + S3_SECRET_KEY
//
// The S3 client uses AWS Signature V4 over stdlib net/http to keep the
// binary small. minio-go would be cleaner; switch when traffic justifies it.

type uploadStore interface {
	// Put writes the file. key is the object key (e.g. "proof_INV123_169...").
	Put(key string, contentType string, body []byte) error
	// Get returns a reader for the file. For S3 callers may prefer Redirect.
	Get(key string) (io.ReadCloser, error)
	// Redirect returns a signed URL the client can fetch directly, if the
	// driver supports it. Empty string means the caller should fall back to
	// Get + ServeContent.
	Redirect(key string, ttl time.Duration) string
}

var uploads uploadStore = &localStore{root: "uploads"}

func initUploads() {
	if os.Getenv("UPLOAD_STORE") != "s3" {
		logger.Info("uploads: local disk")
		return
	}
	cfg := s3Config{
		endpoint:  strings.TrimRight(os.Getenv("S3_ENDPOINT"), "/"),
		bucket:    os.Getenv("S3_BUCKET"),
		region:    os.Getenv("S3_REGION"),
		accessKey: os.Getenv("S3_ACCESS_KEY"),
		secretKey: os.Getenv("S3_SECRET_KEY"),
	}
	if cfg.endpoint == "" || cfg.bucket == "" || cfg.accessKey == "" || cfg.secretKey == "" {
		logger.Warn("UPLOAD_STORE=s3 but endpoint/bucket/key not set; falling back to local")
		return
	}
	if cfg.region == "" {
		cfg.region = "us-east-1"
	}
	uploads = &s3Store{cfg: cfg, http: &http.Client{Timeout: 30 * time.Second}}
	logger.Info("uploads: s3", "endpoint", cfg.endpoint, "bucket", cfg.bucket)
}

// ── Local ────────────────────────────────────────────────────────────────────

type localStore struct{ root string }

func (l *localStore) Put(key, contentType string, body []byte) error {
	if err := os.MkdirAll(l.root, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(l.root, key), body, 0o644)
}

func (l *localStore) Get(key string) (io.ReadCloser, error) {
	f, err := os.Open(filepath.Join(l.root, key))
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (l *localStore) Redirect(key string, ttl time.Duration) string { return "" }

// ── S3 (AWS Signature V4) ───────────────────────────────────────────────────

type s3Config struct {
	endpoint, bucket, region, accessKey, secretKey string
}

type s3Store struct {
	cfg  s3Config
	http *http.Client
}

func (s *s3Store) objectURL(key string) string {
	// Virtual-host style is fine for AWS and DO Spaces; path style is
	// universally supported. Use path style to stay compatible with MinIO
	// and B2.
	return fmt.Sprintf("%s/%s/%s", s.cfg.endpoint, s.cfg.bucket, url.PathEscape(key))
}

func (s *s3Store) Put(key, contentType string, body []byte) error {
	objectURL := s.objectURL(key)
	req, err := http.NewRequest("PUT", objectURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if err := s.sign(req, body); err != nil {
		return err
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("s3 put %d: %s", resp.StatusCode, string(msg))
	}
	return nil
}

func (s *s3Store) Get(key string) (io.ReadCloser, error) {
	req, err := http.NewRequest("GET", s.objectURL(key), nil)
	if err != nil {
		return nil, err
	}
	if err := s.sign(req, nil); err != nil {
		return nil, err
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, fmt.Errorf("s3 get %d", resp.StatusCode)
	}
	return resp.Body, nil
}

// Redirect generates a presigned GET URL. The browser then fetches the file
// straight from S3, eliminating the API as a bandwidth bottleneck.
func (s *s3Store) Redirect(key string, ttl time.Duration) string {
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	t := time.Now().UTC()
	date := t.Format("20060102")
	stamp := t.Format("20060102T150405Z")
	credential := s.cfg.accessKey + "/" + date + "/" + s.cfg.region + "/s3/aws4_request"

	host := s.cfg.endpoint
	if i := strings.Index(host, "://"); i >= 0 {
		host = host[i+3:]
	}

	params := url.Values{}
	params.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	params.Set("X-Amz-Credential", credential)
	params.Set("X-Amz-Date", stamp)
	params.Set("X-Amz-Expires", fmt.Sprintf("%d", int(ttl.Seconds())))
	params.Set("X-Amz-SignedHeaders", "host")

	canonicalRequest := strings.Join([]string{
		"GET",
		"/" + s.cfg.bucket + "/" + url.PathEscape(key),
		params.Encode(),
		"host:" + host + "\n",
		"host",
		"UNSIGNED-PAYLOAD",
	}, "\n")
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		stamp,
		date + "/" + s.cfg.region + "/s3/aws4_request",
		hex.EncodeToString(sha256sum([]byte(canonicalRequest))),
	}, "\n")
	sig := hex.EncodeToString(hmacSHA256(s.signingKey(date), []byte(stringToSign)))
	params.Set("X-Amz-Signature", sig)

	return s.cfg.endpoint + "/" + s.cfg.bucket + "/" + url.PathEscape(key) + "?" + params.Encode()
}

// sign mutates req in place to add an AWS V4 signature. Implements the
// "Authorization header" variant (vs presigned URL). Payload hash is sent
// as x-amz-content-sha256.
func (s *s3Store) sign(req *http.Request, body []byte) error {
	t := time.Now().UTC()
	date := t.Format("20060102")
	stamp := t.Format("20060102T150405Z")

	if req.URL.Host == "" {
		return errors.New("s3 sign: no host on request URL")
	}
	bodyHash := hex.EncodeToString(sha256sum(body))
	req.Header.Set("Host", req.URL.Host)
	req.Header.Set("X-Amz-Date", stamp)
	req.Header.Set("X-Amz-Content-Sha256", bodyHash)

	canonicalHeaders := "host:" + req.URL.Host + "\n" +
		"x-amz-content-sha256:" + bodyHash + "\n" +
		"x-amz-date:" + stamp + "\n"
	signedHeaders := "host;x-amz-content-sha256;x-amz-date"
	canonicalRequest := strings.Join([]string{
		req.Method,
		req.URL.EscapedPath(),
		req.URL.RawQuery,
		canonicalHeaders,
		signedHeaders,
		bodyHash,
	}, "\n")
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		stamp,
		date + "/" + s.cfg.region + "/s3/aws4_request",
		hex.EncodeToString(sha256sum([]byte(canonicalRequest))),
	}, "\n")
	sig := hex.EncodeToString(hmacSHA256(s.signingKey(date), []byte(stringToSign)))
	req.Header.Set("Authorization",
		"AWS4-HMAC-SHA256 Credential="+s.cfg.accessKey+"/"+date+"/"+s.cfg.region+"/s3/aws4_request, "+
			"SignedHeaders="+signedHeaders+", "+
			"Signature="+sig)
	return nil
}

func (s *s3Store) signingKey(date string) []byte {
	k1 := hmacSHA256([]byte("AWS4"+s.cfg.secretKey), []byte(date))
	k2 := hmacSHA256(k1, []byte(s.cfg.region))
	k3 := hmacSHA256(k2, []byte("s3"))
	return hmacSHA256(k3, []byte("aws4_request"))
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func sha256sum(b []byte) []byte {
	h := sha256.New()
	h.Write(b)
	return h.Sum(nil)
}
