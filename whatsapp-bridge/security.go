package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"mime"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Token shared with the MCP server; every REST request must send it as a Bearer token
const tokenFile = "store/api_token"

// Largest file /api/send will read from the outbox
const maxMediaSize = 100 << 20

// Address for the REST API. Only loopback addresses are accepted, so the API is never reachable from the LAN.
func bridgeAddr() (string, error) {
	addr := os.Getenv("WHATSAPP_BRIDGE_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}

	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("invalid WHATSAPP_BRIDGE_ADDR %q: %v", addr, err)
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return "", fmt.Errorf("WHATSAPP_BRIDGE_ADDR must be a loopback IP (e.g. 127.0.0.1:8080), got %q", addr)
	}
	return addr, nil
}

// Read the API token, generating a new random one (mode 0600) on first run
func loadOrCreateToken() (string, error) {
	data, err := os.ReadFile(tokenFile)
	if err == nil {
		if token := strings.TrimSpace(string(data)); len(token) >= 32 {
			return token, os.Chmod(tokenFile, 0600)
		}
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("failed to read %s: %v", tokenFile, err)
	}

	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("failed to generate API token: %v", err)
	}
	token := hex.EncodeToString(buf)
	if err := os.WriteFile(tokenFile, []byte(token+"\n"), 0600); err != nil {
		return "", fmt.Errorf("failed to write %s: %v", tokenFile, err)
	}
	return token, nil
}

// Wrap a handler so it only accepts authenticated, non-browser JSON POSTs
func requireAuth(token string, next http.HandlerFunc) http.HandlerFunc {
	expected := []byte("Bearer " + token)

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Browsers send these on cross-site requests; the MCP server never does
		if r.Header.Get("Origin") != "" || r.Header.Get("Sec-Fetch-Site") != "" {
			http.Error(w, "Browser requests are not allowed", http.StatusForbidden)
			return
		}

		if mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mediaType != "application/json" {
			http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
			return
		}

		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), expected) != 1 {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		next(w, r)
	}
}

// Directory that /api/send is allowed to read media from (default ~/WhatsApp-outbox)
func outboxDir() (string, error) {
	dir := os.Getenv("WHATSAPP_OUTBOX_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, "WhatsApp-outbox")
	}

	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("failed to create outbox %s: %v", dir, err)
	}
	return filepath.EvalSymlinks(dir)
}

// Resolve a media path and make sure it is a regular file inside the outbox, after following symlinks
func resolveOutboxPath(outbox, path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("media path must be absolute")
	}

	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("media file not found: %v", err)
	}

	rel, err := filepath.Rel(outbox, resolved)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("media files must be inside %s", outbox)
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("media file not found: %v", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("media path is not a regular file")
	}
	if info.Size() > maxMediaSize {
		return "", fmt.Errorf("media file is larger than %d MB", maxMediaSize>>20)
	}
	return resolved, nil
}

var unsafeFilenameChars = regexp.MustCompile(`[^A-Za-z0-9._ -]`)

// Turn a sender-controlled name into a single safe path component
func safeFilename(name, fallback string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = unsafeFilenameChars.ReplaceAllString(name, "_")
	name = strings.TrimLeft(name, ". ")
	if len(name) > 120 {
		name = name[len(name)-120:]
	}
	if name == "" || name == "." || name == ".." {
		return fallback
	}
	return name
}
