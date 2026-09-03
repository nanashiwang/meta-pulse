package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

type readinessStub struct{ err error }

func (r readinessStub) Check(context.Context) error { return r.err }

func TestHealthzDoesNotDependOnReadiness(t *testing.T) {
	router := NewRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), readinessStub{err: errors.New("database down")})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("healthz status = %d, want %d", response.Code, http.StatusOK)
	}
}

func TestReadyzReflectsDependencies(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	for _, tc := range []struct {
		name       string
		err        error
		wantStatus int
	}{
		{name: "ready", wantStatus: http.StatusOK},
		{name: "dependency failure", err: errors.New("redis down"), wantStatus: http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			NewRouter(logger, readinessStub{err: tc.err}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
			if response.Code != tc.wantStatus {
				t.Fatalf("readyz status = %d, want %d", response.Code, tc.wantStatus)
			}
		})
	}
}
