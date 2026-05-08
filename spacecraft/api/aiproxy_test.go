package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/absmach/orbitron/pkg/orbitron"
	"github.com/absmach/orbitron/spacecraft/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestQueryHealthAI_MissingKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")

	svc, router := makeRouter()
	svc.On("Telemetry", mock.Anything, mock.AnythingOfType("time.Time")).
		Return(orbitron.Telemetry{Sequence: 1}, nil).Maybe()

	req := httptest.NewRequest(http.MethodGet, "/query-health-ai", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "ANTHROPIC_API_KEY")
}

func TestQueryHealthAI_TelemetryUnavailable(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test-key")

	svc, router := makeRouter()
	svc.On("Telemetry", mock.Anything, mock.AnythingOfType("time.Time")).
		Return(orbitron.Telemetry{}, assert.AnError)

	req := httptest.NewRequest(http.MethodGet, "/query-health-ai", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Contains(t, rec.Body.String(), "telemetry unavailable")
}

func TestQueryHealthAI_RateLimit(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")

	// Use a fresh router per test so the rate limiter is not shared
	svc := makeRateLimitRouter()
	_ = svc

	// Each router has its own limiter; we need to call the same one 11 times
	_, router := makeRouter()
	// Expect Telemetry to be called up to 10 times (missing key means no Telemetry call)
	// Rate limit fires at >10 requests per minute per IP

	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/query-health-ai", nil)
		req.RemoteAddr = "127.0.0.1:9999"
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		assert.NotEqual(t, http.StatusTooManyRequests, rec.Code,
			"request %d should not be rate-limited", i+1)
	}

	req := httptest.NewRequest(http.MethodGet, "/query-health-ai", nil)
	req.RemoteAddr = "127.0.0.1:9999"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusTooManyRequests, rec.Code, "11th request should be rate-limited")
}

// makeRateLimitRouter returns a handler backed by a fresh Service mock.
func makeRateLimitRouter() http.Handler {
	_, router := makeRouter()
	return router
}

// Ensure the handler is registered and responds to GET /query-health-ai.
func TestQueryHealthAI_RouteExists(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")

	_, router := makeRouter()
	req := httptest.NewRequest(http.MethodGet, "/query-health-ai", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	// Should not 404 — any response (even 500) means the route exists
	assert.NotEqual(t, http.StatusNotFound, rec.Code)
}

// blank import to confirm time package used
var _ = time.Now
var _ = api.NewRouter
