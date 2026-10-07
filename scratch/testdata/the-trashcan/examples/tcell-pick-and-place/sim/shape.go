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

// +build example

package sim

import (
	"fmt"
	"math"
)

type (
	Shape interface {
		Position() (x, y int32)
		SetPosition(x, y int32)
		// Size returns the dimensions of the smallest rectangle that will encompass the entirety of the Shape, where
		// Position is the top left corner
		Size() (w, h int32)
		// Center returns the center of the area consumed by this shape
		Center() (x, y int32)
		Closest(x, y int32) (cx, cy int32)
		// Distance returns the shortest distance between the receiver and another shape, note that it may be an
		// approximation, e.g. using Center of shape and Closest of the receiver (if shape isn't regular)
		Distance(shape Shape) float64
		// Collides returns if the given shape collides with the receiver, note that it MAY attempt to call Collides
		// of the shape (if the receiver cannot handle the value WARNING: MUST GUARD AGAINST CYCLES), and may be an
		// approximation, e.g. using Position and Size of shape to form a rectangle, checked against the receiver
		Collides(shape Shape) bool
		Clone() Shape
	}

	shapeRectangle struct{ X, Y, W, H int32 }

	shapeCollidesCycleGuard struct {
		Shape
		cycle bool
	}

	shapeUnpacker interface{ unpack() Shape }
)

var (
	_ Shape = (*shapeRectangle)(nil)
)

func newRectangle(x, y, w, h int32) Shape {
	if w <= 0 || h <= 0 {
		panic(fmt.Errorf(`sim.newRectangle invalid input: %d, %d, %d %d`, x, y, w, h))
	}
	return &shapeRectangle{x, y, w, h}
}

func (s *shapeRectangle) Position() (int32, int32) { return s.X, s.Y }
func (s *shapeRectangle) SetPosition(x, y int32)   { s.X, s.Y = x, y }
func (s *shapeRectangle) Size() (int32, int32)     { return s.W, s.H }
func (s *shapeRectangle) Center() (int32, int32)   { return s.W/2 + s.X, s.H/2 + s.Y }
func (s *shapeRectangle) Closest(x, y int32) (int32, int32) {
	return closestBounds(s.X, s.W, x), closestBounds(s.Y, s.H, y)
}
func (s *shapeRectangle) Distance(shape Shape) float64 {
	// note this may only be approximate for irregular shapes / shapes that aren't rectangles
	return s.distanceCenter(shape)
}
func (s *shapeRectangle) distanceCenter(shape Shape) float64 {
	var (
		x1, y1 = s.Closest(shape.Center())
		x2, y2 = shape.Closest(s.Center())
	)
	return calcDistance(float64(x1), float64(y1), float64(x2), float64(y2))
}
func (s *shapeRectangle) Collides(shape Shape) bool {
	switch shape := unpackShape(shape).(type) {
	case *shapeRectangle:
		return s.collidesRectangle(shape)
	}
	if collides, cycle := s.collidesShape(shape); collides || !cycle {
		return collides
	}
	return s.collidesApproximate(shape)
}
func (s *shapeRectangle) collidesRectangle(shape *shapeRectangle) bool {
	if s.X >= shape.X+shape.W || shape.X >= s.X+s.W {
		return false
	}
	if s.Y+s.H <= shape.Y || shape.Y+shape.H <= s.Y {
		return false
	}
	return true
}
func (s *shapeRectangle) collidesShape(shape Shape) (collides bool, cycle bool) {
	g := &shapeCollidesCycleGuard{Shape: s}
	collides = shape.Collides(g)
	cycle = g.cycle
	return
}
func (s *shapeRectangle) collidesApproximate(shape Shape) bool {
	var r shapeRectangle
	r.X, r.Y = shape.Position()
	r.W, r.H = shape.Size()
	return s.collidesRectangle(&r)
}
func (s *shapeRectangle) Clone() Shape {
	v := *s
	return &v
}

func (s *shapeCollidesCycleGuard) Collides(Shape) bool {
	s.cycle = true
	return false
}
func (s *shapeCollidesCycleGuard) unpack() Shape { return s.Shape }

func unpackShape(shape Shape) Shape {
	for {
		if v, ok := shape.(shapeUnpacker); ok {
			shape = v.unpack()
			continue
		}
		return shape
	}
}

func RoundPosition(x, y float64) (vx, vy int32) { return int32(math.Round(x)), int32(math.Round(y)) }

func calcDistance(x1, y1, x2, y2 float64) float64 {
	return math.Sqrt(math.Pow(x1-x2, 2) + math.Pow(y1-y2, 2))
}

func closestBounds(pos, size, target int32) int32 {
	if size <= 0 {
		panic(size)
	}
	if target <= pos {
		return pos
	}
	if max := pos + size - 1; target > max {
		return max
	}
	return target
}

func sizeInt32(w, h int) (int32, int32) { return int32(w), int32(h) }
