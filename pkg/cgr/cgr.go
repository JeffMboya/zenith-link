// Package cgr implements Contact Graph Routing (RFC 9177) for the Orbitron
// relay mesh. It computes the minimum-latency forwarding path through a set
// of timed contacts between nodes (relays and ground stations).
//
// Usage:
//
//	plan := cgr.ContactPlan{}
//	plan.Add(cgr.Contact{From: "relay-noaa20", To: "gs-nairobi", AOS: ..., LOS: ..., RateBps: 20e6})
//	route := cgr.Compute(plan, "relay-noaa20", "gs-nairobi", time.Now(), 4096)
//	if route != nil {
//	    fmt.Println("next hop:", route.NextHop)
//	}
package cgr

import (
	"encoding/json"
	"time"
)

// NodeID is a string identifier for a node in the contact graph.
// Convention: "gs-nairobi", "relay-noaa20", "relay-sentinel2a".
type NodeID = string

const (
	// MaxHops is the maximum number of relay hops a bundle may traverse.
	MaxHops = 8

	// DefaultGSRateBps is the assumed relay→GS downlink rate (20 Mbps X/S-band).
	DefaultGSRateBps = 20_000_000.0

	// DefaultISLRateBps is the assumed relay→relay inter-satellite link rate.
	DefaultISLRateBps = 100_000_000.0
)

// Contact describes a directed communication opportunity between two nodes
// within a time window, with a given data rate. Reserved tracks bytes already
// committed to this contact window so the router avoids over-committing.
type Contact struct {
	From     NodeID    `json:"from"`
	To       NodeID    `json:"to"`
	AOS      time.Time `json:"aos"`
	LOS      time.Time `json:"los"`
	RateBps  float64   `json:"rate_bps"`
	Reserved float64   `json:"reserved"`
}

// Duration returns the length of the contact window.
func (c Contact) Duration() time.Duration { return c.LOS.Sub(c.AOS) }

// Capacity returns the total byte capacity of the contact.
func (c Contact) Capacity() float64 { return (c.RateBps / 8) * c.Duration().Seconds() }

// Available returns unreserved bytes remaining on this contact.
func (c Contact) Available() float64 {
	if avail := c.Capacity() - c.Reserved; avail > 0 {
		return avail
	}
	return 0
}

// ContactPlan is the set of known contacts in the temporal contact graph.
// It is safe to copy by value; all mutations use pointer receivers.
type ContactPlan struct {
	Node      NodeID    `json:"node"`
	UpdatedAt time.Time `json:"updated_at"`
	Contacts  []Contact `json:"contacts"`
}

// Add appends a contact to the plan, replacing any existing entry with the
// same (From, To, AOS) key.
func (p *ContactPlan) Add(c Contact) {
	for i, e := range p.Contacts {
		if e.From == c.From && e.To == c.To && e.AOS.Equal(c.AOS) {
			p.Contacts[i] = c
			return
		}
	}
	p.Contacts = append(p.Contacts, c)
}

// Merge incorporates all contacts from other into p. Existing entries are
// updated when the (From, To, AOS) key matches.
func (p *ContactPlan) Merge(other ContactPlan) {
	for _, c := range other.Contacts {
		p.Add(c)
	}
}

// Prune removes contacts whose LOS is before cutoff.
func (p *ContactPlan) Prune(cutoff time.Time) {
	kept := p.Contacts[:0]
	for _, c := range p.Contacts {
		if c.LOS.After(cutoff) {
			kept = append(kept, c)
		}
	}
	p.Contacts = kept
}

// Reserve reduces available capacity on the matching contact by bytes.
func (p *ContactPlan) Reserve(from, to NodeID, aos time.Time, bytes float64) {
	for i, c := range p.Contacts {
		if c.From == from && c.To == to && c.AOS.Equal(aos) {
			p.Contacts[i].Reserved += bytes
			return
		}
	}
}

// Route is the computed forwarding path for a bundle.
type Route struct {
	NextHop NodeID    // first node to hand the bundle to
	Arrival time.Time // earliest time the bundle reaches dst
	Hops    []Contact // ordered contact sequence
}

// Compute performs modified Dijkstra on the temporal contact graph to find the
// route from src to dst that minimises earliest bundle arrival time.
//
// Only contacts that are still open at or after now are considered. Volume
// constraints are enforced: a contact must have Available() >= bundleBytes.
//
// Returns nil when no feasible route exists within MaxHops.
func Compute(plan ContactPlan, src, dst NodeID, now time.Time, bundleBytes int) *Route {
	// earliest[node] is the earliest time a bundle originating at src can
	// arrive at node, given the contacts available from now onward.
	earliest := map[NodeID]time.Time{src: now}
	// pred[node] is the contact used to first reach node on the optimal path.
	pred := map[NodeID]Contact{}
	// hops[node] counts relays traversed to reach node.
	hops := map[NodeID]int{src: 0}

	// Bellman-Ford over MaxHops iterations. For small graphs (<100 contacts)
	// this is fast enough; a priority queue is not needed.
	for range MaxHops {
		improved := false
		for _, c := range plan.Contacts {
			arrAtFrom, reachable := earliest[c.From]
			if !reachable {
				continue
			}
			// Contact must still be open when we arrive at c.From.
			if !c.LOS.After(arrAtFrom) {
				continue
			}
			// Volume check.
			if c.Available() < float64(bundleBytes) {
				continue
			}
			// Hop limit.
			if hops[c.From]+1 > MaxHops {
				continue
			}
			// Earliest departure = max(arrival at from, AOS).
			depart := arrAtFrom
			if c.AOS.After(depart) {
				depart = c.AOS
			}
			// Transmission time (seconds).
			txSec := float64(bundleBytes) / (c.RateBps / 8)
			arrAtTo := depart.Add(time.Duration(txSec * float64(time.Second)))

			cur, exists := earliest[c.To]
			if !exists || arrAtTo.Before(cur) {
				earliest[c.To] = arrAtTo
				pred[c.To] = c
				hops[c.To] = hops[c.From] + 1
				improved = true
			}
		}
		if !improved {
			break
		}
	}

	arrival, ok := earliest[dst]
	if !ok {
		return nil
	}

	// Reconstruct contact sequence from pred map.
	var path []Contact
	node := dst
	for node != src {
		c, ok := pred[node]
		if !ok {
			break
		}
		path = append([]Contact{c}, path...)
		node = c.From
	}

	nextHop := dst
	if len(path) > 0 {
		nextHop = path[0].To
	}

	return &Route{NextHop: nextHop, Arrival: arrival, Hops: path}
}

// MarshalJSON implements json.Marshaler so ContactPlan can be served directly.
func (p ContactPlan) MarshalJSON() ([]byte, error) {
	type alias ContactPlan
	return json.Marshal(alias(p))
}
