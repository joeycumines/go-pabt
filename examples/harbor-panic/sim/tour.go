//go:build example
// +build example

package sim

import (
	"context"
	"encoding/json"
	"io"
)

type Beat struct {
	Name  string `json:"name"`
	Ticks int    `json:"ticks"`
}

type TourResult struct {
	Beats   []Beat `json:"beats"`
	RevBefore uint64 `json:"revBefore"`
	RevAfter  uint64 `json:"revAfter"`
}

func DefaultBeats() []Beat {
	return []Beat{
		{Name: "calm-start", Ticks: 50},
		{Name: "storm-hits", Ticks: 50},
		{Name: "human-steals", Ticks: 50},
		{Name: "belief-reveal", Ticks: 50},
		{Name: "fleet-scale", Ticks: 50},
	}
}

func RunTour(ctx context.Context, h *Harbor, beats []Beat) (*TourResult, error) {
	result := &TourResult{
		Beats:     beats,
		RevBefore: h.Revision(),
	}
	for _, beat := range beats {
		for t := 0; t < beat.Ticks; t++ {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
			if err := h.Step(ctx); err != nil {
				return nil, err
			}
		}
	}
	result.RevAfter = h.Revision()
	return result, nil
}

func WriteTourJSON(w io.Writer, result *TourResult) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}
