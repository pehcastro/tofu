package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOrderAccepted(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader(`{"id":"o1","lines":[{"sku":"a","quantity":1,"unit_cents":100}]}`))
	rec := httptest.NewRecorder()
	Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
}

func TestEmptyOrderRefused(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader(`{"id":"o2"}`))
	rec := httptest.NewRecorder()
	Routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d", rec.Code)
	}
}
