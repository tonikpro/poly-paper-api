package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	clob "github.com/tonikpro/poly-paper-api/api/generated/clob"
	"github.com/tonikpro/poly-paper-api/internal/trading"
)

func TestFeeRateEndpointsProxyOfficialShapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		path         string
		expectedPath string
	}{
		{
			name:         "query parameter variant",
			path:         "/fee-rate?token_id=123",
			expectedPath: "/fee-rate?token_id=123",
		},
		{
			name:         "path parameter variant",
			path:         "/fee-rate/123",
			expectedPath: "/fee-rate/123",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.URL.RequestURI(); got != tt.expectedPath {
					t.Fatalf("unexpected upstream request path: got %q want %q", got, tt.expectedPath)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(w, `{"base_fee":30}`)
			}))
			defer upstream.Close()

			srv := NewCLOBServer(nil, nil, trading.NewProxyHandler(upstream.URL))
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()

			clob.Handler(srv).ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("unexpected status: got %d want %d", rec.Code, http.StatusOK)
			}
			if got := rec.Body.String(); got != `{"base_fee":30}` {
				t.Fatalf("unexpected body: got %q", got)
			}
		})
	}
}

func TestPublicProxyCompatibilityVariants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		path         string
		expectedPath string
		body         string
	}{
		{
			name:         "tick size path variant",
			path:         "/tick-size/123",
			expectedPath: "/tick-size/123",
			body:         `"0.01"`,
		},
		{
			name:         "prices query variant",
			path:         "/prices?token_ids=1,2&sides=BUY,SELL",
			expectedPath: "/prices?token_ids=1,2&sides=BUY,SELL",
			body:         `{"1":{"BUY":0.45},"2":{"SELL":0.52}}`,
		},
		{
			name:         "last trades prices query variant",
			path:         "/last-trades-prices?token_ids=1,2",
			expectedPath: "/last-trades-prices?token_ids=1,2",
			body:         `[{"token_id":"1","price":"0.45","side":"BUY"}]`,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if got := r.URL.RequestURI(); got != tt.expectedPath {
					t.Fatalf("unexpected upstream request path: got %q want %q", got, tt.expectedPath)
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(w, tt.body)
			}))
			defer upstream.Close()

			srv := NewCLOBServer(nil, nil, trading.NewProxyHandler(upstream.URL))
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()

			clob.Handler(srv).ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("unexpected status: got %d want %d", rec.Code, http.StatusOK)
			}
			if got := rec.Body.String(); got != tt.body {
				t.Fatalf("unexpected body: got %q want %q", got, tt.body)
			}
		})
	}
}
