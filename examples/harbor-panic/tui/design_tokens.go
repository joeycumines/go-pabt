//go:build example
// +build example

package tui

import (
	"fmt"
	"math"

	"github.com/gdamore/tcell/v2"
	hsim "github.com/joeycumines/go-pabt/examples/harbor-panic/sim"
)

type TokenDef struct {
	Name       string
	Kind       hsim.SpriteKind
	Foreground tcell.Color
	Background tcell.Color
	Bold       bool
}

func sRGBToLinear(c uint8) float64 {
	v := float64(c) / 255.0
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

func relativeLuminance(r, g, b uint8) float64 {
	return 0.2126*sRGBToLinear(r) + 0.7152*sRGBToLinear(g) + 0.0722*sRGBToLinear(b)
}

func ContrastRatio(fg, bg tcell.Color) float64 {
	fr, fg2, fb := fg.RGB()
	br, bg2, bb := bg.RGB()
	l1 := relativeLuminance(uint8(fr), uint8(fg2), uint8(fb))
	l2 := relativeLuminance(uint8(br), uint8(bg2), uint8(bb))
	lighter := math.Max(l1, l2)
	darker := math.Min(l1, l2)
	return (lighter + 0.05) / (darker + 0.05)
}

var DesignTokens = []TokenDef{
	{Name: "wall", Kind: hsim.KindWall, Foreground: tcell.NewRGBColor(140, 150, 165), Background: tcell.ColorBlack},
	{Name: "goal", Kind: hsim.KindGoal, Foreground: tcell.ColorLightBlue, Background: tcell.ColorBlack, Bold: true},
	{Name: "cube-R", Kind: hsim.KindCube, Foreground: tcell.ColorRed, Background: tcell.ColorBlack, Bold: true},
	{Name: "cube-G", Kind: hsim.KindCube, Foreground: tcell.NewRGBColor(0, 210, 0), Background: tcell.ColorBlack, Bold: true},
	{Name: "cube-B", Kind: hsim.KindCube, Foreground: tcell.NewRGBColor(100, 149, 237), Background: tcell.ColorBlack, Bold: true},
	{Name: "cube-Y", Kind: hsim.KindCube, Foreground: tcell.ColorYellow, Background: tcell.ColorBlack, Bold: true},
	{Name: "cube-M", Kind: hsim.KindCube, Foreground: tcell.ColorFuchsia, Background: tcell.ColorBlack, Bold: true},
	{Name: "cube-C", Kind: hsim.KindCube, Foreground: tcell.ColorAqua, Background: tcell.ColorBlack, Bold: true},
	{Name: "actor", Kind: hsim.KindActor, Foreground: tcell.ColorYellow, Background: tcell.ColorBlack, Bold: true},
	{Name: "human", Kind: hsim.KindHumanForklift, Foreground: tcell.ColorRed, Background: tcell.ColorBlack, Bold: true},
}

func PrintAuditTable() {
	bg := tcell.ColorBlack
	fmt.Printf("%-12s %-8s %-10s %s\n", "Token", "Kind", "Contrast", "Pass(>4.5)")
	fmt.Println("--------------------------------------------")
	for _, tok := range DesignTokens {
		ratio := ContrastRatio(tok.Foreground, bg)
		pass := "FAIL"
		if ratio >= 4.5 {
			pass = "PASS"
		}
		fmt.Printf("%-12s %-8d %-10.2f %s\n", tok.Name, tok.Kind, ratio, pass)
	}
}
