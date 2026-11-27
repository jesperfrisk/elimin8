package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVersionReportsEmbeddedRevisionOnly(t *testing.T) {
	c := &VersionController{revision: func() string { return "abc1234" }}
	rec := httptest.NewRecorder()
	c.Version(rec, httptest.NewRequest(http.MethodGet, "/api/version", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 1 || body["revision"] != "abc1234" {
		t.Fatalf("unexpected body %v", body)
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing no-store")
	}
}

func TestVersionUnknownFallback(t *testing.T) {
	rec := httptest.NewRecorder()
	NewVersionController().Version(rec, httptest.NewRequest(http.MethodGet, "/api/version", nil))
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["revision"] != "unknown" {
		t.Fatalf("got %v", body)
	}
}
