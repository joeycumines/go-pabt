// Copyright 2021 Joseph Cumines
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

//go:build example
// +build example

package tui

import (
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/joeycumines/go-pabt/examples/reefer-handoff/sim"
)

var (
	titleStyle    = tcell.StyleDefault.Foreground(tcell.NewRGBColor(220, 220, 240)).Background(tcell.NewRGBColor(20, 20, 60)).Bold(true)
	labelStyle    = tcell.StyleDefault.Foreground(tcell.NewRGBColor(100, 110, 130))
	valueStyle    = tcell.StyleDefault.Foreground(tcell.NewRGBColor(200, 210, 230))
	headerStyle   = tcell.StyleDefault.Foreground(tcell.NewRGBColor(180, 190, 210)).Bold(true)
	pausedStyle   = tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(tcell.ColorYellow).Bold(true)
	helpKeyStyle  = tcell.StyleDefault.Foreground(tcell.NewRGBColor(200, 200, 220)).Bold(true)
	helpDescStyle = tcell.StyleDefault.Foreground(tcell.NewRGBColor(100, 110, 130))
)

func renderAll(screen tcell.Screen, world sim.World, ctrl Controller, records map[string][]RecordSummary) {
	w, h := screen.Size()
	if w < 40 || h < 10 {
		return
	}
	screen.Clear()

	y := renderTitleBar(screen, w, world, ctrl)
	y = renderReeferTable(screen, w, y, world)
	y = renderBayState(screen, w, y, world)
	y = renderLaneState(screen, w, y, world)
	y = renderTractorJobs(screen, w, y, world)
	y = renderRecordTail(screen, w, h-2, y, records)
	renderHelpBar(screen, w, h-1)
}

func renderTitleBar(screen tcell.Screen, width int, world sim.World, ctrl Controller) int {
	cutoffRemaining := sim.CutoffRound - world.Round()
	if cutoffRemaining < 0 {
		cutoffRemaining = 0
	}
	title := fmt.Sprintf(" REEFER HANDOFF  Round %-4d  Cutoff %-4d  Phase %-8s",
		world.Round(), cutoffRemaining, world.Phase())
	for x, ch := range title {
		if x >= width {
			break
		}
		screen.SetContent(x, 0, ch, nil, titleStyle)
	}
	for x := len(title); x < width; x++ {
		screen.SetContent(x, 0, ' ', nil, titleStyle)
	}
	if ctrl != nil && ctrl.Paused() {
		msg := " PAUSED "
		for i, ch := range msg {
			if width-10+i < width {
				screen.SetContent(width-10+i, 0, ch, nil, pausedStyle)
			}
		}
	}
	if ctrl != nil {
		spd := fmt.Sprintf(" %.1fx ", ctrl.Speed())
		for i, ch := range spd {
			if width-20+i < width {
				screen.SetContent(width-20+i, 0, ch, nil, valueStyle)
			}
		}
	}
	return 1
}

func renderReeferTable(screen tcell.Screen, width, y int, world sim.World) int {
	header := "REEFER          TEMP   AT              CUSTODIAN   TAG    DELIVERED"
	drawLine(screen, width, y, header, headerStyle)
	y++
	for _, r := range world.Reefers() {
		delivered := "no"
		if world.Delivered(r.ID) {
			delivered = "YES"
		}
		tag := "ok"
		if !r.TagReadable {
			tag = "BAD"
		}
		cust := string(r.Custodian)
		if cust == "" {
			cust = "-"
		}
		line := fmt.Sprintf("%-14s %4d°C  %-14s  %-10s  %-5s  %s",
			r.ID, r.TempC, r.At, cust, tag, delivered)
		drawLine(screen, width, y, line, valueStyle)
		y++
	}
	return y + 1
}

func renderBayState(screen tcell.Screen, width, y int, world sim.World) int {
	drawLine(screen, width, y, "TRANSFER BAY", headerStyle)
	y++
	occupant := "-"
	for _, t := range world.Tractors() {
		if t.Hold != "" {
			occupant = fmt.Sprintf("%s holding %s", t.ID, t.Hold)
			break
		}
	}
	drawLine(screen, width, y, fmt.Sprintf("  Occupant: %s", occupant), valueStyle)
	return y + 2
}

func renderLaneState(screen tcell.Screen, width, y int, world sim.World) int {
	drawLine(screen, width, y, "LANES", headerStyle)
	y++
	locked := world.Topology().LockedLanes()
	if len(locked) == 0 {
		drawLine(screen, width, y, "  All lanes open", valueStyle)
	} else {
		for _, l := range locked {
			drawLine(screen, width, y, fmt.Sprintf("  LOCKED: %s", l), valueStyle)
			y++
		}
	}
	return y + 1
}

func renderTractorJobs(screen tcell.Screen, width, y int, world sim.World) int {
	drawLine(screen, width, y, "TRACTORS", headerStyle)
	y++
	for _, t := range world.Tractors() {
		job := string(t.Job)
		if job == "" {
			job = "-"
		}
		hold := string(t.Hold)
		if hold == "" {
			hold = "-"
		}
		line := fmt.Sprintf("  %-6s  at=%-12s  job=%-8s  hold=%s", t.ID, t.At, job, hold)
		drawLine(screen, width, y, line, valueStyle)
		y++
	}
	return y + 1
}

func renderRecordTail(screen tcell.Screen, width, maxY, y int, records map[string][]RecordSummary) int {
	if y >= maxY {
		return y
	}
	drawLine(screen, width, y, "COMMITTED RECORDS (tail)", headerStyle)
	y++
	for tugID, recs := range records {
		if y >= maxY {
			break
		}
		count := len(recs)
		last := "none"
		if count > 0 {
			r := recs[count-1]
			last = fmt.Sprintf("seq=%d kind=%s outcome=%s", r.Sequence, r.Kind, r.Outcome)
		}
		drawLine(screen, width, y, fmt.Sprintf("  %s: %d records, last: %s", tugID, count, last), valueStyle)
		y++
	}
	return y
}

func renderHelpBar(screen tcell.Screen, width, y int) {
	help := []struct{ key, desc string }{
		{"q", "quit"}, {"p", "pause/resume"}, {"s", "step"}, {"+/-", "speed"},
	}
	x := 0
	for i, h := range help {
		if x >= width {
			break
		}
		if i > 0 && x < width {
			screen.SetContent(x, y, '│', nil, labelStyle)
			x++
		}
		for _, ch := range h.key {
			if x < width {
				screen.SetContent(x, y, ch, nil, helpKeyStyle)
				x++
			}
		}
		if x < width {
			screen.SetContent(x, y, ':', nil, labelStyle)
			x++
		}
		for _, ch := range h.desc {
			if x < width {
				screen.SetContent(x, y, ch, nil, helpDescStyle)
				x++
			}
		}
		if x < width {
			screen.SetContent(x, y, ' ', nil, tcell.StyleDefault)
			x++
		}
	}
}

func drawLine(screen tcell.Screen, width, y int, text string, style tcell.Style) {
	for x, ch := range text {
		if x >= width {
			break
		}
		screen.SetContent(x, y, ch, nil, style)
	}
}
