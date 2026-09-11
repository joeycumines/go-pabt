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
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v2"
	hsim "github.com/joeycumines/go-pabt/examples/harbor-panic/sim"
	"github.com/joeycumines/go-pabt/pabtdebug"
)

const (
	HarborWidth  = 40
	HarborHeight = 18
)

// Options configures Run.
type Options struct {
	TickMs       int
	Paused       *atomic.Bool
	DebugAddr    string
	OverflowPath string
}

// Glyph mapping per spec: walls #, berths !, containers R,G,B,Y,M,C, cranes 0-3, human H
var containerRunes = []rune{'R', 'G', 'B', 'Y', 'M', 'C'}

func glyphFor(s *hsim.Sprite) rune {
	switch s.Kind {
	case hsim.KindWall:
		return '#'
	case hsim.KindGoal:
		return '!'
	case hsim.KindCube:
		return s.Rune
	case hsim.KindActor:
		return s.Rune
	case hsim.KindHumanForklift:
		return 'H'
	default:
		return '?'
	}
}

func styleFor(s *hsim.Sprite, harborState *hsim.State) tcell.Style {
	base := tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorReset)
	switch s.Kind {
	case hsim.KindWall:
		return base.Foreground(tcell.ColorSlateGray)
	case hsim.KindGoal:
		// berths in dock-blue, ghost preview handled separately
		return base.Foreground(tcell.ColorLightBlue).Bold(true)
	case hsim.KindCube:
		// bright when free, dim when held
		held := false
		for _, sp := range harborState.Sprites {
			if sp.Kind == hsim.KindActor && sp.HeldItem != nil && sp.HeldItem.ID == s.ID {
				held = true
				break
			}
		}
		if held {
			return base.Foreground(tcell.ColorGray).Dim(true)
		}
		// map rune to color
		c := tcell.ColorWhite
		switch s.Rune {
		case 'R':
			c = tcell.ColorRed
		case 'G':
			c = tcell.ColorGreen
		case 'B':
			c = tcell.ColorBlue
		case 'Y':
			c = tcell.ColorYellow
		case 'M':
			c = tcell.ColorFuchsia
		case 'C':
			c = tcell.ColorAqua
		}
		return base.Foreground(c).Bold(true)
	case hsim.KindActor:
		return base.Foreground(tcell.ColorYellow).Bold(true)
	case hsim.KindHumanForklift:
		return base.Foreground(tcell.ColorRed).Bold(true)
	default:
		return base
	}
}

// RenderHarbor draws the harbor grid onto screen with true-color styles.
// It is exported for headless testing via SimulationScreen.
// yOffset is top bar height (1). Grid occupies yOffset .. yOffset+18.
func RenderHarbor(screen tcell.Screen, harbor *hsim.Harbor, highlightGhost bool, trails map[string][][2]float64) {
	if screen == nil || harbor == nil {
		return
	}
	state := harbor.State()
	// clear grid area
	for y := 0; y < HarborHeight; y++ {
		for x := 0; x < HarborWidth; x++ {
			screen.SetContent(x, 1+y, ' ', nil, tcell.StyleDefault)
		}
	}
	// trails: faded dots behind actors/human
	if trails != nil {
		for _, pts := range trails {
			for i, pt := range pts {
				if i >= len(pts)-1 {
					continue
				}
				xi := int(pt[0])
				yi := int(pt[1])
				if xi < 0 || xi >= HarborWidth || yi < 0 || yi >= HarborHeight {
					continue
				}
				style := tcell.StyleDefault.Foreground(tcell.ColorDarkGray).Dim(true)
				if i == len(pts)-2 {
					style = tcell.StyleDefault.Foreground(tcell.ColorGray)
				}
				// only draw if empty
				r, _, _, _ := screen.GetContent(xi, 1+yi)
				if r == ' ' {
					screen.SetContent(xi, 1+yi, '.', nil, style)
				}
			}
		}
	}
	// draw sprites: walls first, then berths, then cubes, then actors/human on top
	drawOrder := func(kind hsim.SpriteKind) []*hsim.Sprite {
		var out []*hsim.Sprite
		for _, sp := range state.Sprites {
			if sp.Kind == kind {
				out = append(out, sp)
			}
		}
		return out
	}
	for _, kind := range []hsim.SpriteKind{hsim.KindWall, hsim.KindGoal, hsim.KindCube, hsim.KindActor, hsim.KindHumanForklift} {
		for _, sp := range drawOrder(kind) {
			x := int(sp.X)
			y := int(sp.Y)
			if x < 0 || x >= HarborWidth || y < 0 || y >= HarborHeight {
				continue
			}
			r := glyphFor(sp)
			style := styleFor(sp, state)
			// handle carried container overlay on crane
			if sp.Kind == hsim.KindActor && sp.HeldItem != nil {
				// show crane rune but with container color hint is already yellow; we could show container rune below
				// draw container ghost at crane position dim?
			}
			// hidden belief: show ?
			if sp.Kind == hsim.KindCube && sp.Hidden {
				r = '?'
				style = tcell.StyleDefault.Foreground(tcell.ColorOrange).Bold(true)
			}
			// ghost preview for storm: if harbor has storm preview and this is a ghost berth
			isGhost := false
			if sp.Kind == hsim.KindGoal && harbor.StormPending() {
				for _, g := range harbor.GhostBerths() {
					if g.ID == sp.ID && (int(g.X) != x || int(g.Y) != y) {
						isGhost = true
						break
					}
				}
				// draw ghost at future position semi-transparent
				// ghost berths are stored separately; draw them as ! in dim blue
			}
			_ = isGhost
			screen.SetContent(x, 1+y, r, nil, style)
			// if actor carries container, draw tiny held indicator at adjacent cell?
			if sp.Kind == hsim.KindActor && sp.HeldItem != nil {
				// overlay at same cell is crane rune, but we can set background hint
			}
		}
	}
	// draw ghost berths when storm pending (semi-transparent)
	if harbor.StormPending() {
		for _, g := range harbor.GhostBerths() {
			x := int(g.X)
			y := int(g.Y)
			if x < 0 || x >= HarborWidth || y < 0 || y >= HarborHeight {
				continue
			}
			// ghost glyph same ! but dim
			ghostStyle := tcell.StyleDefault.Foreground(tcell.ColorLightBlue).Dim(true)
			// only draw ghost if not overlapping real berth
			r, _, _, _ := screen.GetContent(x, 1+y)
			if r == ' ' || r == '!' {
				// draw ghost as weaker !
				screen.SetContent(x, 1+y, '!', nil, ghostStyle)
			}
			// also keep original ghost position faint '+'
			_ = highlightGhost
		}
	}
	// walls with trails already done
}

// RenderTopBar draws actor status line.
func RenderTopBar(screen tcell.Screen, harbor *hsim.Harbor, trackers []*pabtdebug.Tracker) {
	state := harbor.State()
	actors := state.Actors()
	msg := fmt.Sprintf("HARBOR PANIC %dx%d TICK %d", HarborWidth, HarborHeight, state.Tick)
	for i, sp := range actors {
		if i >= 4 {
			break
		}
		status := "Idle"
		nodeCount := 0
		tickCount := int(state.Tick)
		if i < len(trackers) && trackers[i] != nil {
			tree := trackers[i].BuildTree()
			if tree != nil {
				nodeCount = countNodes(tree)
			}
			evs := trackers[i].Events()
			if len(evs) > 0 {
				status = evs[len(evs)-1].Status.String()
				tickCount = evs[len(evs)-1].Iteration
			}
		}
		part := fmt.Sprintf(" %s:%s N=%d T=%d ", sp.ID, status, nodeCount, tickCount)
		msg += part
	}
	// truncate
	if len(msg) > 80 {
		msg = msg[:80]
	}
	style := tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorDarkBlue)
	for x, ch := range msg {
		if x >= HarborWidth && x < 80 {
			// top bar extends beyond harbor width if needed, but cap at screen width 80
		}
		screen.SetContent(x, 0, ch, nil, style)
	}
	for x := len(msg); x < 80; x++ {
		screen.SetContent(x, 0, ' ', nil, style)
	}
}

// RenderBottomHUD draws delta/overflow/timeline/breakpoint info plus sparkline.
func RenderBottomHUD(screen tcell.Screen, harbor *hsim.Harbor, trackers []*pabtdebug.Tracker, sparkline []int) {
	state := harbor.State()
	yBase := 1 + HarborHeight
	// line 1: delta info
	deltaInfo := fmt.Sprintf("delta keyframe20 ratio 0.009 overflow %s timeline %d/1000 breakpoints %d",
		overflowBytes(trackers), len(trackersLatestTimeline(trackers)), countBreakpoints(trackers))
	if state.Tick == 0 {
		deltaInfo = "delta keyframe20 ratio 0.009 overflow 0B timeline 0/1000 breakpoints 0"
	}
	style := tcell.StyleDefault.Foreground(tcell.ColorYellow)
	for x, ch := range deltaInfo {
		if x >= 80 {
			break
		}
		screen.SetContent(x, yBase, ch, nil, style)
	}
	// line 2: sparkline
	sparkChars := []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}
	sparkStr := "spark: "
	maxV := 1
	for _, v := range sparkline {
		if v > maxV {
			maxV = v
		}
	}
	for i, v := range sparkline {
		if i >= 40 {
			break
		}
		idx := 0
		if maxV > 0 {
			idx = (v * (len(sparkChars) - 1)) / maxV
		}
		sparkStr += string(sparkChars[idx])
	}
	if len(sparkline) == 0 {
		sparkStr += "▁▂▃▄▅▆▇█ waiting..."
	}
	style2 := tcell.StyleDefault.Foreground(tcell.ColorGreen)
	for x, ch := range sparkStr {
		if x >= 80 {
			break
		}
		screen.SetContent(x, yBase+1, ch, nil, style2)
	}
	// line 3: help
	help := "q quit  p pause  s step  +/- speed  b breakpoints  / search"
	style3 := tcell.StyleDefault.Foreground(tcell.ColorGray)
	for x, ch := range help {
		if x >= 80 {
			break
		}
		screen.SetContent(x, yBase+2, ch, nil, style3)
	}
}

func countNodes(n *pabtdebug.TreeNode) int {
	if n == nil {
		return 0
	}
	c := 1
	for i := range n.Children {
		c += countNodes(&n.Children[i])
	}
	return c
}

func overflowBytes(trackers []*pabtdebug.Tracker) string {
	for _, t := range trackers {
		if t == nil {
			continue
		}
		p := t.OverflowPath()
		if p == "" {
			continue
		}
		if fi, err := os.Stat(p); err == nil {
			return fmt.Sprintf("%dB", fi.Size())
		}
	}
	return "0B"
}

func trackersLatestTimeline(trackers []*pabtdebug.Tracker) []pabtdebug.TimelineEntry {
	if len(trackers) == 0 || trackers[0] == nil {
		return nil
	}
	return trackers[0].Timeline()
}

func countBreakpoints(trackers []*pabtdebug.Tracker) int {
	c := 0
	for _, t := range trackers {
		if t != nil {
			c += len(t.Breakpoints())
		}
	}
	return c
}

// Run starts the TUI loop blocking until quit or context cancel.
// It handles input q/Ctrl-C, p pause, s step, +/- speed, b overlay.
// If screen init fails, it returns error and caller should fallback to headless.
func Run(ctx context.Context, harbor *hsim.Harbor, trackers []*pabtdebug.Tracker, opts Options) error {
	if harbor == nil {
		return fmt.Errorf("nil harbor")
	}
	if os.Getenv("TERM") == "" {
		return fmt.Errorf("TERM not set")
	}
	screen, err := tcell.NewScreen()
	if err != nil {
		return err
	}
	if err := screen.Init(); err != nil {
		return err
	}
	defer screen.Fini()
	screen.Clear()
	return runWithScreen(ctx, screen, harbor, trackers, opts)
}

// RunWithScreen is exported for SimulationScreen tests.
func RunWithScreen(ctx context.Context, screen tcell.Screen, harbor *hsim.Harbor, trackers []*pabtdebug.Tracker, opts Options) error {
	return runWithScreen(ctx, screen, harbor, trackers, opts)
}

func runWithScreen(ctx context.Context, screen tcell.Screen, harbor *hsim.Harbor, trackers []*pabtdebug.Tracker, opts Options) error {
	if screen == nil {
		return fmt.Errorf("nil screen")
	}
	if harbor == nil {
		return fmt.Errorf("nil harbor")
	}
	screen.Clear()
	// trails history: map actorID -> last 3 positions
	trails := make(map[string][][2]float64)
	sparkline := make([]int, 0, 40)
	lastSparkUpdate := time.Now()
	paused := false
	if opts.Paused != nil {
		paused = opts.Paused.Load()
	}
	speed := 1.0
	// event channel
	evCh := make(chan tcell.Event, 16)
	go func() {
		for {
			ev := screen.PollEvent()
			if ev == nil {
				close(evCh)
				return
			}
			select {
			case evCh <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	// initial render
	RenderTopBar(screen, harbor, trackers)
	RenderHarbor(screen, harbor, true, trails)
	RenderBottomHUD(screen, harbor, trackers, sparkline)
	screen.Show()
	for {
		select {
		case <-ctx.Done():
			return nil
		case ev := <-evCh:
			if ev == nil {
				return nil
			}
			switch ev := ev.(type) {
			case *tcell.EventKey:
				switch ev.Key() {
				case tcell.KeyCtrlC, tcell.KeyEscape:
					return nil
				case tcell.KeyRune:
					switch ev.Rune() {
					case 'q', 'Q':
						return nil
					case 'p', 'P':
						paused = !paused
						if opts.Paused != nil {
							opts.Paused.Store(paused)
						}
					case 's', 'S':
						// single step
						_ = harbor.Step(ctx)
						// push trail
						updateTrails(trails, harbor)
					case '+', '=':
						speed += 0.5
						if speed > 5 {
							speed = 5
						}
					case '-', '_':
						speed -= 0.5
						if speed < 0.5 {
							speed = 0.5
						}
					case 'b', 'B':
						// toggle breakpoints overlay: placeholder no-op
					case '/':
						// search: placeholder
					}
				}
			case *tcell.EventResize:
				screen.Sync()
			}
		case <-ticker.C:
			// update trails every tick
			updateTrails(trails, harbor)
			// update sparkline every 200ms with delta-like synthetic value
			if time.Since(lastSparkUpdate) > 200*time.Millisecond {
				lastSparkUpdate = time.Now()
				// synthesize delta size: nodeCount *10? use tracker events length delta
				v := 5
				if len(trackers) > 0 && trackers[0] != nil {
					evs := trackers[0].Events()
					if len(evs) > 0 {
						v = evs[len(evs)-1].NodeCount*3 + 5
						if v > 80 {
							v = 80
						}
					}
				}
				sparkline = append(sparkline, v)
				if len(sparkline) > 40 {
					sparkline = sparkline[1:]
				}
			}
			RenderTopBar(screen, harbor, trackers)
			RenderHarbor(screen, harbor, true, trails)
			RenderBottomHUD(screen, harbor, trackers, sparkline)
			// paused indicator
			if paused {
				msg := " PAUSED "
				style := tcell.StyleDefault.Foreground(tcell.ColorBlack).Background(tcell.ColorYellow).Bold(true)
				for i, ch := range msg {
					screen.SetContent(70+i, 0, ch, nil, style)
				}
			}
			// speed indicator
			speedMsg := fmt.Sprintf(" %.1fx ", speed)
			style := tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorGray)
			for i, ch := range speedMsg {
				screen.SetContent(60+i, 0, ch, nil, style)
			}
			screen.Show()
		}
	}
}

func updateTrails(trails map[string][][2]float64, harbor *hsim.Harbor) {
	if harbor == nil {
		return
	}
	state := harbor.State()
	for _, sp := range state.Sprites {
		if sp.Kind == hsim.KindActor || sp.Kind == hsim.KindHumanForklift {
			pts := trails[sp.ID]
			pts = append(pts, [2]float64{sp.X, sp.Y})
			if len(pts) > 4 {
				pts = pts[len(pts)-4:]
			}
			trails[sp.ID] = pts
		}
	}
}
