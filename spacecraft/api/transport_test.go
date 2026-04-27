package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/absmach/zenith-link/pkg/ccsds/tcframe"
	"github.com/absmach/zenith-link/pkg/errors"
	"github.com/absmach/zenith-link/pkg/orbital"
	"github.com/absmach/zenith-link/pkg/zenith"
	"github.com/absmach/zenith-link/spacecraft"
	"github.com/absmach/zenith-link/spacecraft/api"
	"github.com/absmach/zenith-link/spacecraft/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func makeRouter() (*mocks.Service, http.Handler) {
	svc := new(mocks.Service)
	return svc, api.NewRouter(svc)
}

func fakeState() spacecraft.State {
	return spacecraft.State{
		Time: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		ECI:  orbital.ECIState{X: 6_788_000, Y: 0, Z: 0},
		Geodetic: orbital.GeodeticPosition{
			LatitudeDeg:  37.7,
			LongitudeDeg: -122.4,
			AltitudeM:    409_863,
		},
		InSunlight: true,
	}
}

func fakeTelemetry() zenith.Telemetry {
	return zenith.Telemetry{
		Sequence:  1,
		Timestamp: uint32(time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC).Unix()),
		Presence:  zenith.PresencePosition,
		LatE7:     377_498_000,
		LonE7:     -1_224_194_000,
		AltM:      415_000,
	}
}

func buildTC(t *testing.T, scid uint16, vcid uint8, data []byte) []byte {
	t.Helper()
	frame := tcframe.TransferFrame{
		Primary: tcframe.PrimaryHeader{
			SCID:                scid,
			VCID:                vcid,
			FrameSequenceNumber: 0,
		},
		DataField: data,
	}
	b, err := tcframe.Encode(frame)
	require.NoError(t, err)
	return b
}

// ─── GET /health ────────────────────────────────────────────────────────────

func TestHealthHandler(t *testing.T) {
	_, router := makeRouter()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Equal(t, "ok", body["status"])
}

// ─── GET /telemetry ─────────────────────────────────────────────────────────

func TestTelemetryHandler(t *testing.T) {
	tm := fakeTelemetry()

	tests := []struct {
		desc       string
		url        string
		setupMock  func(svc *mocks.Service)
		wantStatus int
		check      func(t *testing.T, body map[string]any)
	}{
		{
			desc: "current time — returns telemetry JSON",
			url:  "/telemetry",
			setupMock: func(svc *mocks.Service) {
				svc.On("Telemetry", mock.Anything, mock.AnythingOfType("time.Time")).
					Return(tm, nil)
			},
			wantStatus: http.StatusOK,
			check: func(t *testing.T, body map[string]any) {
				assert.Equal(t, float64(1), body["sequence"])
				assert.Equal(t, float64(zenith.PresencePosition), body["presence"])
			},
		},
		{
			desc: "explicit t query param — passes correct time",
			url:  "/telemetry?t=1700000000",
			setupMock: func(svc *mocks.Service) {
				svc.On("Telemetry", mock.Anything, time.Unix(1700000000, 0).UTC()).
					Return(tm, nil)
			},
			wantStatus: http.StatusOK,
		},
		{
			desc: "telemetry with inference result includes label",
			url:  "/telemetry",
			setupMock: func(svc *mocks.Service) {
				inf := tm
				inf.Presence |= zenith.PresenceInference
				inf.InferenceClass = 2 // land
				inf.InferenceConf = 200
				svc.On("Telemetry", mock.Anything, mock.AnythingOfType("time.Time")).
					Return(inf, nil)
			},
			wantStatus: http.StatusOK,
			check: func(t *testing.T, body map[string]any) {
				assert.Equal(t, "land", body["inference_label"])
				assert.NotNil(t, body["inference_class"])
				assert.NotNil(t, body["inference_conf"])
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, router := makeRouter()
			tc.setupMock(svc)

			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			assert.Equal(t, tc.wantStatus, rec.Code)
			if tc.check != nil {
				var body map[string]any
				require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
				tc.check(t, body)
			}
			svc.AssertExpectations(t)
		})
	}
}

// ─── GET /state ─────────────────────────────────────────────────────────────

func TestStateHandler(t *testing.T) {
	st := fakeState()

	tests := []struct {
		desc       string
		url        string
		setupMock  func(svc *mocks.Service)
		wantStatus int
		check      func(t *testing.T, body map[string]any)
	}{
		{
			desc: "returns orbital state JSON",
			url:  "/state",
			setupMock: func(svc *mocks.Service) {
				svc.On("State", mock.Anything, mock.AnythingOfType("time.Time")).
					Return(st, nil)
			},
			wantStatus: http.StatusOK,
			check: func(t *testing.T, body map[string]any) {
				assert.Contains(t, body, "lat_deg")
				assert.Contains(t, body, "lon_deg")
				assert.Contains(t, body, "alt_m")
				assert.Equal(t, true, body["in_sunlight"])
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, router := makeRouter()
			tc.setupMock(svc)

			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			assert.Equal(t, tc.wantStatus, rec.Code)
			if tc.check != nil {
				var body map[string]any
				require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
				tc.check(t, body)
			}
			svc.AssertExpectations(t)
		})
	}
}

// ─── GET /frame ─────────────────────────────────────────────────────────────

func TestTMFrameHandler(t *testing.T) {
	tests := []struct {
		desc       string
		url        string
		setupMock  func(svc *mocks.Service)
		wantStatus int
		check      func(t *testing.T, body []byte)
	}{
		{
			desc: "default size 512",
			url:  "/frame",
			setupMock: func(svc *mocks.Service) {
				svc.On("TMFrame", mock.Anything, mock.AnythingOfType("time.Time"), 512).
					Return(make([]byte, 512), nil)
			},
			wantStatus: http.StatusOK,
			check: func(t *testing.T, body []byte) {
				assert.Equal(t, 512, len(body))
			},
		},
		{
			desc: "custom size 1024",
			url:  "/frame?size=1024",
			setupMock: func(svc *mocks.Service) {
				svc.On("TMFrame", mock.Anything, mock.AnythingOfType("time.Time"), 1024).
					Return(make([]byte, 1024), nil)
			},
			wantStatus: http.StatusOK,
			check: func(t *testing.T, body []byte) {
				assert.Equal(t, 1024, len(body))
			},
		},
		{
			desc:       "non-numeric size returns 400",
			url:        "/frame?size=abc",
			setupMock:  func(_ *mocks.Service) {},
			wantStatus: http.StatusBadRequest,
		},
		{
			desc:       "size below minimum returns 400",
			url:        "/frame?size=5",
			setupMock:  func(_ *mocks.Service) {},
			wantStatus: http.StatusBadRequest,
		},
		{
			desc:       "size above maximum returns 400",
			url:        "/frame?size=2049",
			setupMock:  func(_ *mocks.Service) {},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, router := makeRouter()
			tc.setupMock(svc)

			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			assert.Equal(t, tc.wantStatus, rec.Code)
			if tc.check != nil {
				tc.check(t, rec.Body.Bytes())
			}
			svc.AssertExpectations(t)
		})
	}
}

// ─── GET /frame/zenith ───────────────────────────────────────────────────────

func TestZenithFrameHandler(t *testing.T) {
	fakeFrame := []byte{0x5A, 0x4C, 0x02, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}

	tests := []struct {
		desc       string
		setupMock  func(svc *mocks.Service)
		wantStatus int
		check      func(t *testing.T, body []byte)
	}{
		{
			desc: "returns raw Zenith-Link binary",
			setupMock: func(svc *mocks.Service) {
				svc.On("TelemetryFrame", mock.Anything, mock.AnythingOfType("time.Time")).
					Return(fakeFrame, nil)
			},
			wantStatus: http.StatusOK,
			check: func(t *testing.T, body []byte) {
				assert.Equal(t, fakeFrame, body)
				assert.Equal(t, len(fakeFrame), len(body))
			},
		},
		{
			desc: "service error returns 500",
			setupMock: func(svc *mocks.Service) {
				svc.On("TelemetryFrame", mock.Anything, mock.AnythingOfType("time.Time")).
					Return([]byte(nil), errors.ErrOrbitalPropagate)
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, router := makeRouter()
			tc.setupMock(svc)

			req := httptest.NewRequest(http.MethodGet, "/frame/zenith", nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			assert.Equal(t, tc.wantStatus, rec.Code)
			if tc.check != nil {
				tc.check(t, rec.Body.Bytes())
			}
			svc.AssertExpectations(t)
		})
	}
}

// ─── POST /command ───────────────────────────────────────────────────────────

func TestCommandHandler(t *testing.T) {
	inferTC := buildTC(t, 0x5A, 0, []byte{0x01}) // CmdInferenceRun
	accepted := spacecraft.CommandResult{CommandID: spacecraft.CmdInferenceRun, Accepted: true, Message: "ok"}

	tests := []struct {
		desc       string
		body       []byte
		setupMock  func(svc *mocks.Service)
		wantStatus int
		check      func(t *testing.T, body map[string]any)
	}{
		{
			desc: "valid TC frame returns command result",
			body: inferTC,
			setupMock: func(svc *mocks.Service) {
				svc.On("ExecuteCommand", mock.Anything, inferTC).Return(accepted, nil)
			},
			wantStatus: http.StatusOK,
			check: func(t *testing.T, body map[string]any) {
				assert.Equal(t, true, body["accepted"])
				assert.Equal(t, float64(spacecraft.CmdInferenceRun), body["command_id"])
			},
		},
		{
			desc: "malformed frame returns 422",
			body: []byte{0x00, 0x01},
			setupMock: func(svc *mocks.Service) {
				svc.On("ExecuteCommand", mock.Anything, []byte{0x00, 0x01}).
					Return(spacecraft.CommandResult{}, errors.ErrMalformedFrame)
			},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			desc: "unknown command returns 422",
			body: buildTC(t, 0x5A, 0, []byte{0xFF}),
			setupMock: func(svc *mocks.Service) {
				svc.On("ExecuteCommand", mock.Anything, mock.Anything).
					Return(spacecraft.CommandResult{}, errors.ErrCommandUnknown)
			},
			wantStatus: http.StatusUnprocessableEntity,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, router := makeRouter()
			tc.setupMock(svc)

			req := httptest.NewRequest(http.MethodPost, "/command",
				bytes.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/octet-stream")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			assert.Equal(t, tc.wantStatus, rec.Code)
			if tc.check != nil {
				var body map[string]any
				require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
				tc.check(t, body)
			}
			svc.AssertExpectations(t)
		})
	}
}

// ─── GET /windows ────────────────────────────────────────────────────────────

func TestWindowsHandler(t *testing.T) {
	fakeWindows := []orbital.ContactWindow{
		{
			AOS:             time.Now().UTC().Add(30 * time.Minute),
			LOS:             time.Now().UTC().Add(38 * time.Minute),
			MaxElevationDeg: 42.5,
		},
	}

	tests := []struct {
		desc       string
		url        string
		setupMock  func(svc *mocks.Service)
		wantStatus int
		check      func(t *testing.T, body map[string]any)
	}{
		{
			desc: "returns contact windows with defaults (Nairobi, 4h, 5°)",
			url:  "/windows",
			setupMock: func(svc *mocks.Service) {
				svc.On("Windows", mock.Anything,
					-1.2864, 36.8172,
					mock.AnythingOfType("time.Time"),
					mock.AnythingOfType("time.Time"),
					5.0).Return(fakeWindows, nil)
			},
			wantStatus: http.StatusOK,
			check: func(t *testing.T, body map[string]any) {
				assert.Equal(t, float64(1), body["count"])
				wins := body["windows"].([]any)
				assert.Len(t, wins, 1)
				w := wins[0].(map[string]any)
				assert.Equal(t, 42.5, w["max_elevation_deg"])
			},
		},
		{
			desc: "custom coordinates and horizon",
			url:  "/windows?gs_lat=51.5&gs_lon=-0.1&hours=2&min_elev=10",
			setupMock: func(svc *mocks.Service) {
				svc.On("Windows", mock.Anything,
					51.5, -0.1,
					mock.AnythingOfType("time.Time"),
					mock.AnythingOfType("time.Time"),
					10.0).Return(fakeWindows, nil)
			},
			wantStatus: http.StatusOK,
		},
		{
			desc:       "invalid gs_lat returns 400",
			url:        "/windows?gs_lat=91",
			setupMock:  func(_ *mocks.Service) {},
			wantStatus: http.StatusBadRequest,
		},
		{
			desc:       "hours exceeds 48 returns 400",
			url:        "/windows?hours=100",
			setupMock:  func(_ *mocks.Service) {},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, router := makeRouter()
			tc.setupMock(svc)

			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			assert.Equal(t, tc.wantStatus, rec.Code)
			if tc.check != nil {
				var body map[string]any
				require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
				tc.check(t, body)
			}
			svc.AssertExpectations(t)
		})
	}
}
