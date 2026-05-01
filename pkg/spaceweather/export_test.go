// export_test.go exposes internal state injection for unit tests.
// This file is compiled only when running tests (it is in package spaceweather,
// not spaceweather_test, so it can access unexported fields).
package spaceweather

import "time"

// InjectForTest sets synthetic Conditions on the monitor without a network fetch.
// Used by unit tests that verify RSSIAdjustmentDB and StormLevel logic in isolation.
func InjectForTest(m *Monitor, kp, solarFlux float64) {
	m.mu.Lock()
	m.conditions = Conditions{
		Kp:        kp,
		SolarFlux: solarFlux,
		FetchedAt: time.Now().UTC(),
		Available: true,
	}
	m.mu.Unlock()
}
