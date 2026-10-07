//go:build example
// +build example

package logic

import (
	"encoding/json"
	"io"

	hsim "github.com/joeycumines/go-pabt/examples/harbor-panic/sim"
)

type TraceEntry struct {
	Tick             int64  `json:"tick"`
	Revision         uint64 `json:"revision"`
	SafetyViolations int    `json:"safetyViolations"`
	Cycles           int    `json:"cycles"`
	DeadEnds         int    `json:"deadEnds"`
	ActorCount       int    `json:"actorCount"`
	CubeCount        int    `json:"cubeCount"`
}

func ExportTrace(harbor *hsim.Harbor, w io.Writer) error {
	st := harbor.State()
	entry := TraceEntry{
		Tick:             st.Tick,
		Revision:         st.Revision,
		SafetyViolations: st.SafetyViolations,
		Cycles:           st.Cycles,
		DeadEnds:         st.DeadEnds,
		ActorCount:       len(st.Actors()),
		CubeCount:        len(st.CubesAll()),
	}
	enc := json.NewEncoder(w)
	return enc.Encode(entry)
}
