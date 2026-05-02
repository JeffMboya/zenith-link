package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/absmach/zenith-link/groundstation"
	"github.com/absmach/zenith-link/groundstation/api"
	"github.com/absmach/zenith-link/groundstation/mocks"
	"github.com/absmach/zenith-link/pkg/errors"
	"github.com/absmach/zenith-link/pkg/zenith"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func makeRouter() (*mocks.Service, http.Handler) {
	svc := new(mocks.Service)
	return svc, api.NewRouter(svc, api.RouterConfig{})
}

func fakeTelemetry() zenith.Telemetry {
	return zenith.Telemetry{
		Sequence:  7,
		Timestamp: 1_700_000_000,
		Presence:  zenith.PresencePosition | zenith.PresenceBatV,
		LatE7:     377_498_000,
		LonE7:     -1_224_194_000,
		AltM:      415_000,
		BatV:      7400,
	}
}

func fakeLatest() groundstation.LatestState {
	return groundstation.LatestState{
		Telemetry:   fakeTelemetry(),
		ReceivedAt:  time.Now().UTC(),
		FrameNumber: 42,
	}
}

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

func TestLatestHandler(t *testing.T) {
	tests := []struct {
		desc       string
		setupMock  func(svc *mocks.Service)
		wantStatus int
		check      func(t *testing.T, body map[string]any)
	}{
		{
			desc: "returns latest telemetry",
			setupMock: func(svc *mocks.Service) {
				svc.On("Latest", mock.Anything).Return(fakeLatest(), nil)
			},
			wantStatus: http.StatusOK,
			check: func(t *testing.T, body map[string]any) {
				tm := body["telemetry"].(map[string]any)
				assert.Equal(t, float64(7), tm["sequence"])
				assert.Equal(t, float64(42), body["frame_number"])
			},
		},
		{
			desc: "returns 204 when no data yet",
			setupMock: func(svc *mocks.Service) {
				svc.On("Latest", mock.Anything).Return(groundstation.LatestState{}, groundstation.ErrNoData)
			},
			wantStatus: http.StatusNoContent,
		},
		{
			desc: "latest with inference result includes label",
			setupMock: func(svc *mocks.Service) {
				st := fakeLatest()
				st.Telemetry.Presence |= zenith.PresenceInference
				st.Telemetry.InferenceClass = 4
				st.Telemetry.InferenceConf = 200
				svc.On("Latest", mock.Anything).Return(st, nil)
			},
			wantStatus: http.StatusOK,
			check: func(t *testing.T, body map[string]any) {
				tm := body["telemetry"].(map[string]any)
				assert.Equal(t, "RF_DEGRADATION", tm["inference_label"])
				assert.NotNil(t, tm["inference_class"])
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, router := makeRouter()
			tc.setupMock(svc)

			req := httptest.NewRequest(http.MethodGet, "/latest", nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			assert.Equal(t, tc.wantStatus, rec.Code)
			if tc.check != nil && rec.Body.Len() > 0 {
				var body map[string]any
				require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
				tc.check(t, body)
			}
			svc.AssertExpectations(t)
		})
	}
}

func TestReceiveHandler(t *testing.T) {
	fakeFrame := []byte{0x5A, 0x4C, 0x02, 0x00, 0x00, 0x01}
	tm := fakeTelemetry()

	tests := []struct {
		desc       string
		body       []byte
		headers    map[string]string
		setupMock  func(svc *mocks.Service)
		wantStatus int
		check      func(t *testing.T, body map[string]any)
	}{
		{
			desc: "valid frame is accepted and decoded telemetry returned",
			body: fakeFrame,
			setupMock: func(svc *mocks.Service) {
				svc.On("Receive", mock.Anything, fakeFrame).Return(tm, nil)
			},
			wantStatus: http.StatusOK,
			check: func(t *testing.T, body map[string]any) {
				assert.Equal(t, float64(7), body["sequence"])
			},
		},
		{
			desc: "relayed frame — X-Relayed-By header surfaced in response",
			body: fakeFrame,
			headers: map[string]string{
				"X-Relayed-By": "SC-2",
			},
			setupMock: func(svc *mocks.Service) {
				svc.On("Receive", mock.Anything, fakeFrame).Return(tm, nil)
			},
			wantStatus: http.StatusOK,
			check: func(t *testing.T, body map[string]any) {
				assert.Equal(t, "SC-2", body["relayed_by"])
			},
		},
		{
			desc: "invalid frame returns 422",
			body: []byte{0x00},
			setupMock: func(svc *mocks.Service) {
				svc.On("Receive", mock.Anything, []byte{0x00}).
					Return(zenith.Telemetry{}, errors.ErrFrameTooSmall)
			},
			wantStatus: http.StatusUnprocessableEntity,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, router := makeRouter()
			tc.setupMock(svc)

			req := httptest.NewRequest(http.MethodPost, "/receive", bytes.NewReader(tc.body))
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
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

func TestCommandHandler(t *testing.T) {
	t.Run("no spacecraft addr configured returns 502", func(t *testing.T) {
		_, router := makeRouter()
		body := `{"command_id": 1}`
		req := httptest.NewRequest(http.MethodPost, "/command", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadGateway, rec.Code)
	})

	t.Run("invalid JSON body returns 400", func(t *testing.T) {
		_, router := makeRouter()
		req := httptest.NewRequest(http.MethodPost, "/command",
			bytes.NewBufferString("not-json"))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("invalid base64 payload returns 400", func(t *testing.T) {
		_, router := makeRouter()
		body := `{"command_id": 3, "payload_b64": "!!!not-base64!!!"}`
		req := httptest.NewRequest(http.MethodPost, "/command", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("command forwarded to spacecraft and result returned", func(t *testing.T) {

		scServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/command", r.URL.Path)
			assert.Equal(t, "application/octet-stream", r.Header.Get("Content-Type"))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"command_id":1,"accepted":true,"message":"inference complete"}`))
		}))
		defer scServer.Close()

		_, scSvc := new(mocks.Service), new(mocks.Service)
		_ = scSvc
		cfg := api.RouterConfig{
			SpacecraftAddr: scServer.URL,
			SCID:           0x5A,
			VCID:           0,
		}
		svc := new(mocks.Service)
		router := api.NewRouter(svc, cfg)

		body := `{"command_id": 1}`
		req := httptest.NewRequest(http.MethodPost, "/command", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		var res map[string]any
		require.NoError(t, json.NewDecoder(rec.Body).Decode(&res))
		assert.Equal(t, true, res["accepted"])
		assert.Equal(t, float64(1), res["command_id"])
	})
}

func TestSubscribeBroadcast(t *testing.T) {
	t.Run("telemetry reaches subscriber after Receive", func(t *testing.T) {
		key := []byte("test-hmac-key-32-bytes-padded-xx")
		svc := groundstation.New(groundstation.Config{HMACKey: key})

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		ch, err := svc.Subscribe(ctx)
		require.NoError(t, err)

		frame, err := zenith.Encode(fakeTelemetry(), key)
		require.NoError(t, err)

		_, err = svc.Receive(context.Background(), frame)
		require.NoError(t, err)

		select {
		case got := <-ch:
			assert.Equal(t, uint16(7), got.Sequence)
		case <-ctx.Done():
			t.Fatal("timeout waiting for broadcast")
		}
	})
}
