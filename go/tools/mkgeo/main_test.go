package main

import "testing"

// TestClipConcave verifies Sutherland–Hodgman keeps concave notches and never
// emits spurious straight edges outside the clip box (REVIEW_B #3).
func TestClipConcave(t *testing.T) {
	// C-shaped concave ring crossing the western clip edge (lon 114).
	ring := []pt{
		{112, 10}, {120, 10}, {120, 20}, {117, 20}, {117, 13}, {115, 13}, {115, 20}, {112, 20},
	}
	out := clipRing(ring)
	if len(out) < 3 {
		t.Fatalf("clipped len = %d, want >=3", len(out))
	}
	for _, p := range out {
		if p.x < clipMinLon-1e-9 || p.x > clipMaxLon+1e-9 || p.y < clipMinLat-1e-9 || p.y > clipMaxLat+1e-9 {
			t.Errorf("point %+v outside clip box", p)
		}
	}
	// Notch (x 115–117, y 13–20) must survive: there must be vertices with
	// x in [115,117] and y near 13 (the notch floor), otherwise the clip
	// collapsed the concavity into a straight edge.
	found := false
	for _, p := range out {
		if p.x >= 115-1e-9 && p.x <= 117+1e-9 && p.y < 15 {
			found = true
		}
	}
	if !found {
		t.Errorf("concave notch lost in clip output %+v", out)
	}
	// Fully outside ring clips to nothing.
	outside := []pt{{100, 10}, {101, 10}, {101, 11}, {100, 11}}
	if got := clipRing(outside); len(got) != 0 {
		t.Errorf("outside ring clipped to %v, want empty", got)
	}
}
