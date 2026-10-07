//go:build example
// +build example

package sim

import "fmt"

type FleetMember struct {
	Harbor *Harbor
	Index  int
}

func NewFleet(n int, seed int64) []FleetMember {
	members := make([]FleetMember, n)
	for i := 0; i < n; i++ {
		h := NewHarbor(0, 3.1, seed+int64(i))
		members[i] = FleetMember{Harbor: h, Index: i}
	}
	return members
}

func FleetPlanID(memberIdx, actorIdx int) string {
	return fmt.Sprintf("fleet-%d-bot-%d", memberIdx, actorIdx)
}
