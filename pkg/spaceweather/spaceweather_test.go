package spaceweather_test

import (
	"testing"

	"github.com/absmach/zenith-link/pkg/spaceweather"
)

func TestRSSIAdjustmentDB(t *testing.T) {
	tests := []struct {
		desc      string
		kp        float64
		solarFlux float64
		wantDB    float64
	}{
		{desc: "Kp=0 quiet — 0 dB", kp: 0, solarFlux: 100, wantDB: 0},
		{desc: "Kp=3 quiet — 0 dB", kp: 3, solarFlux: 100, wantDB: 0},
		{desc: "Kp=4 minor — -1 dB", kp: 4, solarFlux: 100, wantDB: -1},
		{desc: "Kp=5 moderate — -3 dB", kp: 5, solarFlux: 100, wantDB: -3},
		{desc: "Kp=6 strong — -6 dB", kp: 6, solarFlux: 100, wantDB: -6},
		{desc: "Kp=7 severe — -10 dB", kp: 7, solarFlux: 100, wantDB: -10},
		{desc: "Kp=8 severe — -10 dB", kp: 8, solarFlux: 100, wantDB: -10},

		{desc: "Kp=3 + solar flux > 180 — +1 dB net", kp: 3, solarFlux: 200, wantDB: 1},
		{desc: "Kp=5 + solar flux > 180 — -2 dB net", kp: 5, solarFlux: 200, wantDB: -2},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			m := spaceweather.NewMonitor()

			spaceweather.InjectForTest(m, tc.kp, tc.solarFlux)

			got := m.RSSIAdjustmentDB()
			if got != tc.wantDB {
				t.Errorf("RSSIAdjustmentDB() = %v, want %v (kp=%.1f flux=%.0f)",
					got, tc.wantDB, tc.kp, tc.solarFlux)
			}
		})
	}
}

func TestStormLevel(t *testing.T) {
	tests := []struct {
		kp   float64
		want string
	}{
		{0, "QUIET"},
		{2, "QUIET"},
		{3.9, "QUIET"},
		{4, "MINOR"},
		{4.9, "MINOR"},
		{5, "MODERATE"},
		{5.9, "MODERATE"},
		{6, "STRONG"},
		{6.9, "STRONG"},
		{7, "SEVERE"},
		{8, "SEVERE"},
		{9, "SEVERE"},
	}

	for _, tc := range tests {
		m := spaceweather.NewMonitor()
		spaceweather.InjectForTest(m, tc.kp, 100)
		got := m.StormLevel()
		if got != tc.want {
			t.Errorf("StormLevel() kp=%.1f = %q, want %q", tc.kp, got, tc.want)
		}
	}
}

func TestConditions_NotAvailableBeforeFirstFetch(t *testing.T) {
	m := spaceweather.NewMonitor()
	c := m.Current()
	if c.Available {
		t.Error("expected Available=false before first fetch, got true")
	}
	if c.Kp != 0 {
		t.Errorf("expected Kp=0 before first fetch, got %v", c.Kp)
	}
}
