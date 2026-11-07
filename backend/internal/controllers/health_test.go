package controllers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jesperfrisk/elimin8/backend/internal/services"
)

type stubDatabase struct {
	err    error
	called bool
	ctx    context.Context
}

func (db *stubDatabase) Ping(ctx context.Context) error {
	db.called = true
	db.ctx = ctx
	return db.err
}

func TestReadiness(t *testing.T) {
	for _, tt := range []struct {
		name       string
		err        error
		wantStatus int
		wantBody   string
	}{
		{"connected", nil, http.StatusOK, `{"status":"ok","database":"connected"}`},
		{"unavailable", errors.New("private connection credentials"), http.StatusServiceUnavailable, `{"status":"unavailable","database":"disconnected"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db := &stubDatabase{err: tt.err}
			controller := NewHealthController(services.NewHealthService(db))
			response := httptest.NewRecorder()
			controller.Readiness(response, httptest.NewRequest(http.MethodGet, "/api/health/ready", nil))
			if response.Code != tt.wantStatus || strings.TrimSpace(response.Body.String()) != tt.wantBody {
				t.Fatalf("got %d %s, want %d %s", response.Code, response.Body.String(), tt.wantStatus, tt.wantBody)
			}
			if response.Header().Get("Content-Type") != "application/json" {
				t.Fatal("expected JSON content type")
			}
			if _, ok := db.ctx.Deadline(); !ok {
				t.Fatal("database check must have a deadline")
			}
		})
	}
}

func TestLivenessDoesNotDependOnDatabase(t *testing.T) {
	db := &stubDatabase{err: errors.New("offline")}
	controller := NewHealthController(services.NewHealthService(db))
	response := httptest.NewRecorder()
	controller.Liveness(response, httptest.NewRequest(http.MethodGet, "/api/health/live", nil))
	if response.Code != http.StatusOK || db.called {
		t.Fatalf("liveness must succeed without checking MongoDB: status=%d database_called=%v", response.Code, db.called)
	}
}
