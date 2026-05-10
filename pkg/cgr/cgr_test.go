package cgr_test

import (
	"testing"
	"time"

	"github.com/absmach/orbitron/pkg/cgr"
)

var t0 = time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

// singleHopPlan returns a plan with one direct contact from src to dst.
func singleHopPlan(src, dst cgr.NodeID, aos, los time.Time, rate float64) cgr.ContactPlan {
	var p cgr.ContactPlan
	p.Add(cgr.Contact{From: src, To: dst, AOS: aos, LOS: los, RateBps: rate})
	return p
}

func TestCompute_DirectRoute(t *testing.T) {
	plan := singleHopPlan("relay-a", "gs-nairobi", t0.Add(10*time.Minute), t0.Add(20*time.Minute), cgr.DefaultGSRateBps)
	route := cgr.Compute(plan, "relay-a", "gs-nairobi", t0, 1024)
	if route == nil {
		t.Fatal("expected a route, got nil")
	}
	if route.NextHop != "gs-nairobi" {
		t.Errorf("NextHop: want gs-nairobi, got %s", route.NextHop)
	}
	if len(route.Hops) != 1 {
		t.Errorf("Hops: want 1, got %d", len(route.Hops))
	}
}

func TestCompute_TwoHopRoute(t *testing.T) {
	// relay-a → relay-b → gs-nairobi
	// Direct relay-a → gs-nairobi contact opens much later.
	var plan cgr.ContactPlan
	// relay-a can reach relay-b now.
	plan.Add(cgr.Contact{From: "relay-a", To: "relay-b", AOS: t0, LOS: t0.Add(15 * time.Minute), RateBps: cgr.DefaultISLRateBps})
	// relay-b can reach gs-nairobi starting in 5 min.
	plan.Add(cgr.Contact{From: "relay-b", To: "gs-nairobi", AOS: t0.Add(5 * time.Minute), LOS: t0.Add(20 * time.Minute), RateBps: cgr.DefaultGSRateBps})
	// relay-a can reach gs-nairobi directly, but only after 60 min.
	plan.Add(cgr.Contact{From: "relay-a", To: "gs-nairobi", AOS: t0.Add(60 * time.Minute), LOS: t0.Add(70 * time.Minute), RateBps: cgr.DefaultGSRateBps})

	route := cgr.Compute(plan, "relay-a", "gs-nairobi", t0, 1024)
	if route == nil {
		t.Fatal("expected a route, got nil")
	}
	// Two-hop via relay-b should arrive earlier than direct at t0+60min.
	if route.Arrival.After(t0.Add(60 * time.Minute)) {
		t.Errorf("expected two-hop route to beat direct at t0+60min, got arrival %v", route.Arrival)
	}
	if len(route.Hops) != 2 {
		t.Errorf("expected 2 hops, got %d", len(route.Hops))
	}
}

func TestCompute_NoRoute(t *testing.T) {
	plan := singleHopPlan("relay-a", "relay-b", t0.Add(10*time.Minute), t0.Add(20*time.Minute), cgr.DefaultGSRateBps)
	route := cgr.Compute(plan, "relay-a", "gs-nairobi", t0, 1024)
	if route != nil {
		t.Errorf("expected nil route, got %+v", route)
	}
}

func TestCompute_VolumeConstraint(t *testing.T) {
	// Contact with only 1 byte of capacity — bundle of 4096 must not fit.
	var plan cgr.ContactPlan
	plan.Add(cgr.Contact{
		From: "relay-a", To: "gs-nairobi",
		AOS: t0.Add(5 * time.Minute), LOS: t0.Add(5*time.Minute + time.Millisecond),
		RateBps: 8, // 1 byte/sec × 0.001 sec = 0.001 bytes capacity
	})
	route := cgr.Compute(plan, "relay-a", "gs-nairobi", t0, 4096)
	if route != nil {
		t.Errorf("expected no route due to volume constraint, got %+v", route)
	}
}

func TestCompute_ExpiredContact(t *testing.T) {
	// Contact LOS is in the past.
	plan := singleHopPlan("relay-a", "gs-nairobi", t0.Add(-30*time.Minute), t0.Add(-10*time.Minute), cgr.DefaultGSRateBps)
	route := cgr.Compute(plan, "relay-a", "gs-nairobi", t0, 1024)
	if route != nil {
		t.Errorf("expected no route for expired contact, got %+v", route)
	}
}

func TestContactPlan_Merge(t *testing.T) {
	var a cgr.ContactPlan
	a.Add(cgr.Contact{From: "relay-a", To: "gs-nairobi", AOS: t0, LOS: t0.Add(time.Hour), RateBps: 1e6})

	var b cgr.ContactPlan
	b.Add(cgr.Contact{From: "relay-b", To: "gs-nairobi", AOS: t0, LOS: t0.Add(time.Hour), RateBps: 1e6})

	a.Merge(b)
	if len(a.Contacts) != 2 {
		t.Errorf("expected 2 contacts after merge, got %d", len(a.Contacts))
	}
}

func TestContactPlan_Prune(t *testing.T) {
	var p cgr.ContactPlan
	p.Add(cgr.Contact{From: "relay-a", To: "gs", AOS: t0.Add(-2 * time.Hour), LOS: t0.Add(-1 * time.Hour), RateBps: 1e6})
	p.Add(cgr.Contact{From: "relay-a", To: "gs", AOS: t0.Add(time.Hour), LOS: t0.Add(2 * time.Hour), RateBps: 1e6})

	p.Prune(t0)
	if len(p.Contacts) != 1 {
		t.Errorf("expected 1 contact after prune, got %d", len(p.Contacts))
	}
	if p.Contacts[0].AOS.Before(t0) {
		t.Errorf("pruned wrong contact")
	}
}

func TestContact_Capacity(t *testing.T) {
	c := cgr.Contact{
		AOS:     t0,
		LOS:     t0.Add(10 * time.Minute),
		RateBps: 20_000_000,
	}
	want := (20_000_000.0 / 8) * 600 // 1.5 GB
	if got := c.Capacity(); got != want {
		t.Errorf("Capacity: want %g, got %g", want, got)
	}
}
