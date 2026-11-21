package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func testClient(url string) *LBClient {
	return &LBClient{BaseURL: url, Token: "testtoken", HTTP: &http.Client{Timeout: 5 * time.Second}}
}

func makeListens(n int) []Listen {
	listens := make([]Listen, n)
	for i := range listens {
		listens[i] = Listen{
			Artist:      fmt.Sprintf("Artist %d", i),
			Title:       fmt.Sprintf("Song %d", i),
			DurationSec: 200,
			ListenedAt:  1782900000 + int64(i),
		}
	}
	return listens
}

func TestSubmitBatches(t *testing.T) {
	var mu sync.Mutex
	var bodies [][]byte
	var auths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		mu.Lock()
		bodies = append(bodies, b)
		auths = append(auths, r.Header.Get("Authorization"))
		mu.Unlock()
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	if err := testClient(srv.URL).Submit(context.Background(), makeListens(1200)); err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 3 {
		t.Fatalf("got %d requests, want 3", len(bodies))
	}
	wantSizes := []int{500, 500, 200}
	for i, body := range bodies {
		var s struct {
			ListenType string            `json:"listen_type"`
			Payload    []json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(body, &s); err != nil {
			t.Fatal(err)
		}
		if s.ListenType != "import" {
			t.Errorf("request %d: listen_type = %q, want import", i, s.ListenType)
		}
		if len(s.Payload) != wantSizes[i] {
			t.Errorf("request %d: %d listens, want %d", i, len(s.Payload), wantSizes[i])
		}
		if auths[i] != "Token testtoken" {
			t.Errorf("request %d: Authorization = %q", i, auths[i])
		}
	}
}

func TestSubmitFieldMapping(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = string(b)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	l := Listen{
		Artist:      "Test Artist",
		Album:       "Test Album",
		Title:       "First Song",
		TrackNumber: "1",
		DurationSec: 245,
		ListenedAt:  1782900000,
		MBID:        "9f0a1c2e-0000-4000-8000-000000000001",
	}
	if err := testClient(srv.URL).Submit(context.Background(), []Listen{l}); err != nil {
		t.Fatal(err)
	}
	want := `{"listen_type":"import","payload":[{"listened_at":1782900000,"track_metadata":{"artist_name":"Test Artist","track_name":"First Song","release_name":"Test Album","additional_info":{"tracknumber":"1","duration_ms":245000,"recording_mbid":"9f0a1c2e-0000-4000-8000-000000000001","media_player":"Rockbox","submission_client":"tide","submission_client_version":"0.1.0"}}}]}`
	if got != want {
		t.Fatalf("body mismatch:\ngot  %s\nwant %s", got, want)
	}
}

func TestSubmitOmitsEmptyFields(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = string(b)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	l := Listen{Artist: "Test Artist", Title: "First Song", DurationSec: 245, ListenedAt: 1782900000}
	if err := testClient(srv.URL).Submit(context.Background(), []Listen{l}); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"release_name", "tracknumber", "recording_mbid"} {
		if strings.Contains(got, key) {
			t.Errorf("body contains %q, want omitted: %s", key, got)
		}
	}
}

func TestSubmitRateLimit(t *testing.T) {
	var mu sync.Mutex
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			w.Header().Set("X-RateLimit-Reset-In", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":"rate limited"}`))
			return
		}
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	if err := testClient(srv.URL).Submit(context.Background(), makeListens(1)); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("got %d requests, want 2", calls)
	}
}

func TestSubmitFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"boom"}`))
	}))
	defer srv.Close()

	err := testClient(srv.URL).Submit(context.Background(), makeListens(1))
	if err == nil {
		t.Fatal("Submit succeeded, want error")
	}
	if !strings.Contains(err.Error(), "500") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("error %q should include status code and body", err)
	}
}

func TestScrobbleFailureKeepsLog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/1/validate-token" {
			w.Write([]byte(`{"valid":true,"user_name":"example"}`))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"boom"}`))
	}))
	defer srv.Close()

	dev := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dev, ".rockbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dev, ".scrobbler.log")
	if err := os.WriteFile(logPath, []byte(logUTC), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LISTENBRAINZ_TOKEN", "testtoken")
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	old := listenBrainzBaseURL
	listenBrainzBaseURL = srv.URL
	defer func() { listenBrainzBaseURL = old }()

	if err := runScrobble(dev, &options{}); err == nil {
		t.Fatal("runScrobble succeeded, want error")
	}
	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("log should still exist after failed submit: %v", err)
	}
}

func TestValidateInvalid(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"valid":false}`))
	}))
	defer srv.Close()

	if _, err := testClient(srv.URL).Validate(context.Background()); err == nil {
		t.Fatal("Validate succeeded, want error for valid:false")
	}
}

func TestValidateValid(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Token testtoken" {
			t.Errorf("Authorization = %q", got)
		}
		w.Write([]byte(`{"valid":true,"user_name":"example"}`))
	}))
	defer srv.Close()

	user, err := testClient(srv.URL).Validate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if user != "example" {
		t.Fatalf("user = %q, want example", user)
	}
}

func TestLoadToken(t *testing.T) {
	t.Setenv("LISTENBRAINZ_TOKEN", "envtoken")
	t.Setenv("LISTENBRAINZ_TOKEN_FILE", "")
	got, err := loadToken()
	if err != nil || got != "envtoken" {
		t.Fatalf("loadToken = %q, %v; want envtoken", got, err)
	}

	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("  filetoken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LISTENBRAINZ_TOKEN", "")
	t.Setenv("LISTENBRAINZ_TOKEN_FILE", path)
	got, err = loadToken()
	if err != nil || got != "filetoken" {
		t.Fatalf("loadToken = %q, %v; want filetoken (trimmed)", got, err)
	}

	t.Setenv("LISTENBRAINZ_TOKEN_FILE", "")
	if _, err := loadToken(); err == nil {
		t.Fatal("loadToken succeeded with no token set, want error")
	}
}
