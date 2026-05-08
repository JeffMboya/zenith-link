package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/absmach/orbitron/pkg/orbitron"
	"github.com/absmach/orbitron/spacecraft"
)

// queryHealthAIHandler streams a Claude AI health analysis via SSE.
// Rate-limited to 10 requests per minute per remote IP.
// Requires ANTHROPIC_API_KEY env var.
func queryHealthAIHandler(svc spacecraft.Service) http.HandlerFunc {
	rl := newRateLimiter(10, time.Minute)
	return func(w http.ResponseWriter, r *http.Request) {
		ip := r.RemoteAddr
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			ip = xff
		}
		if !rl.allow(ip) {
			http.Error(w, `{"error":"rate limit exceeded — try again in 60s"}`, http.StatusTooManyRequests)
			return
		}

		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			http.Error(w, `{"error":"AI service unavailable — ANTHROPIC_API_KEY not configured"}`, http.StatusInternalServerError)
			return
		}

		// Build telemetry snapshot from current state
		tm, err := svc.Telemetry(r.Context(), time.Now())
		if err != nil {
			http.Error(w, `{"error":"telemetry unavailable"}`, http.StatusServiceUnavailable)
			return
		}

		spacecraftName := os.Getenv("SPACECRAFT_NAME")
		if spacecraftName == "" {
			spacecraftName = "spacecraft"
		}

		prompt := buildHealthPrompt(spacecraftName, tm)

		// Set SSE headers before streaming
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()

		if err := streamClaude(ctx, apiKey, prompt, w, flusher); err != nil {
			fmt.Fprintf(w, "data: [ERROR] %s\n\n", err.Error())
			flusher.Flush()
		}

		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}
}

func buildHealthPrompt(name string, tm orbitron.Telemetry) string {
	inferenceLabel := orbitron.InferenceClassName(tm.InferenceClass)
	confPct := float64(tm.InferenceConf) / 255.0 * 100.0
	return fmt.Sprintf(
		`You are the onboard AI health monitor for %s. Analyze this telemetry snapshot and provide a concise health assessment.

TELEMETRY:
- Inference class: %s (confidence: %.0f%%)
- Battery: %.2f V (solar: %.2f V)
- Attitude: pitch=%.1f° yaw=%.1f° roll=%.1f°
- Temperature: %.1f°C
- RSSI: %.0f dBm
- Sequence: %d

Respond in 2-3 sentences in aerospace ops voice. Lead with the most important finding.
If anomalous, recommend a specific command (/safe-mode, /nominal, /set-pitch-threshold, etc.).
Format: "[AI Analysis]: <assessment>"`,
		name,
		inferenceLabel, confPct,
		float64(tm.BatV)/1000.0, float64(tm.SolarV)/1000.0,
		float64(tm.AttPitch)/100.0, float64(tm.AttYaw)/100.0, float64(tm.AttRoll)/100.0,
		float64(tm.TempC)/100.0,
		float64(tm.RSSI)/10.0,
		tm.Sequence,
	)
}

// streamClaude calls Claude API with streaming and writes SSE events.
func streamClaude(ctx context.Context, apiKey, prompt string, w http.ResponseWriter, flusher http.Flusher) error {
	reqBody, _ := json.Marshal(map[string]any{
		"model":      "claude-sonnet-4-6",
		"max_tokens": 300,
		"stream":     true,
		"system":     "You are a terse aerospace flight operations AI. Respond only with the health assessment. Never exceed 3 sentences.",
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.anthropic.com/v1/messages",
		bytes.NewReader(reqBody))
	if err != nil {
		return fmt.Errorf("request build failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("AI service unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("API key invalid — contact ops")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("Claude API error %d: %s", resp.StatusCode, body)
	}

	// Parse Claude streaming SSE events
	decoder := json.NewDecoder(resp.Body)
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("AI stream timeout")
		default:
		}

		var event struct {
			Type  string `json:"type"`
			Delta *struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
		}

		if err := decoder.Decode(&event); err != nil {
			if err == io.EOF {
				return nil
			}
			return nil // ignore parse errors mid-stream
		}

		if event.Type == "content_block_delta" && event.Delta != nil && event.Delta.Type == "text_delta" {
			fmt.Fprintf(w, "data: %s\n\n", event.Delta.Text)
			flusher.Flush()
		}
	}
}

// rateLimiter: simple in-memory sliding window per IP.
type rateLimiter struct {
	mu      sync.Mutex
	counts  map[string][]time.Time
	limit   int
	window  time.Duration
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{counts: make(map[string][]time.Time), limit: limit, window: window}
}

func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-rl.window)
	ts := rl.counts[key]
	valid := ts[:0]
	for _, t := range ts {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	if len(valid) >= rl.limit {
		rl.counts[key] = valid
		return false
	}
	rl.counts[key] = append(valid, now)
	return true
}
