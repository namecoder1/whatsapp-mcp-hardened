package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSafeFilename(t *testing.T) {
	cases := map[string]string{
		"../../../.zshrc":       "zshrc",
		"..\\..\\evil.exe":      "evil.exe",
		"/etc/passwd":           "passwd",
		"fattura marzo.pdf":     "fattura marzo.pdf",
		"..":                    "fallback",
		"":                      "fallback",
		"rapporto\x00.pdf":      "rapporto_.pdf",
		"s.whatsapp.net@123_45": "s.whatsapp.net_123_45",
	}
	for in, want := range cases {
		if got := safeFilename(in, "fallback"); got != want {
			t.Errorf("safeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveOutboxPath(t *testing.T) {
	root := t.TempDir()
	outbox := filepath.Join(root, "outbox")
	os.Mkdir(outbox, 0700)
	outbox, _ = filepath.EvalSymlinks(outbox)

	inside := filepath.Join(outbox, "foto.jpg")
	os.WriteFile(inside, []byte("x"), 0600)
	secret := filepath.Join(root, "secret")
	os.WriteFile(secret, []byte("x"), 0600)
	os.Symlink(secret, filepath.Join(outbox, "link.jpg"))
	os.Mkdir(filepath.Join(outbox, "dir"), 0700)

	if _, err := resolveOutboxPath(outbox, inside); err != nil {
		t.Errorf("file inside outbox rejected: %v", err)
	}
	for _, p := range []string{
		secret,
		filepath.Join(outbox, "..", "secret"),
		filepath.Join(outbox, "link.jpg"),
		filepath.Join(outbox, "dir"),
		"foto.jpg",
	} {
		if _, err := resolveOutboxPath(outbox, p); err == nil {
			t.Errorf("resolveOutboxPath(%q) accepted, want error", p)
		}
	}
}

func TestRequireAuth(t *testing.T) {
	handler := requireAuth("secret-token", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	request := func(mutate func(*http.Request)) int {
		r := httptest.NewRequest(http.MethodPost, "/api/send", strings.NewReader("{}"))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer secret-token")
		mutate(r)
		w := httptest.NewRecorder()
		handler(w, r)
		return w.Code
	}

	if code := request(func(r *http.Request) {}); code != http.StatusOK {
		t.Errorf("valid request got %d", code)
	}
	checks := map[string]func(*http.Request){
		"no token":    func(r *http.Request) { r.Header.Del("Authorization") },
		"wrong token": func(r *http.Request) { r.Header.Set("Authorization", "Bearer nope") },
		"browser":     func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") },
		"fetch site":  func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") },
		"text/plain":  func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") },
		"GET":         func(r *http.Request) { r.Method = http.MethodGet },
	}
	for name, mutate := range checks {
		if code := request(mutate); code == http.StatusOK {
			t.Errorf("%s: request accepted", name)
		}
	}
}

func TestListenSocket(t *testing.T) {
	t.Chdir(t.TempDir())
	os.Mkdir("store", 0700)
	os.WriteFile(socketFile, []byte("stale"), 0644)

	listener, err := listenSocket()
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	info, err := os.Stat(socketFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		t.Error("stale file was not replaced by a socket")
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("socket permissions = %o, want 600", perm)
	}
}
