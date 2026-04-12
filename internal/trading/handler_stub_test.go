package trading

import (
	"net/http"
	"net/http/httptest"
	"testing"

	clob "github.com/tonikpro/poly-paper-api/api/generated/clob"
)

func TestPostHeartbeatReturnsOfficialEnvelope(t *testing.T) {
	t.Parallel()

	handler := NewCLOBTradingHandler(nil)
	req := httptest.NewRequest(http.MethodPost, "/v1/heartbeats", nil)
	rec := httptest.NewRecorder()

	handler.PostHeartbeat(rec, req, clob.PostHeartbeatParams{})

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: got %d want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); got != "{\"heartbeat_id\":\"paper-heartbeat\"}\n" {
		t.Fatalf("unexpected body: got %q", got)
	}
}
