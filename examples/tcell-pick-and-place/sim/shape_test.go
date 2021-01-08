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
	"testing"
)

func Test_closestBounds(t *testing.T) {
	for _, tc := range []struct{ Pos, Size, Target, Result int32 }{
		{0, 1, 0, 0},
		{0, 1, -1, 0},
		{0, 1, 1, 0},
		{3, 2, 3, 3},
		{3, 2, 2, 3},
		{3, 2, 4, 4},
		{3, 2, 5, 4},
	} {
		t.Run(fmt.Sprintf(`%d_%d_%d`, tc.Pos, tc.Size, tc.Target), func(t *testing.T) {
			if result := closestBounds(tc.Pos, tc.Size, tc.Target); result != tc.Result {
				t.Error(result)
			}
		})
	}
}

func TestShapeRectangle_Collides(t *testing.T) {
	for _, tc := range []struct {
		R *shapeRectangle
		S Shape
		C bool
	}{
		{&shapeRectangle{}, &shapeRectangle{}, false},
		{&shapeRectangle{0, 0, 1, 1}, &shapeRectangle{0, 0, 1, 1}, true},
		{&shapeRectangle{1, 0, 1, 1}, &shapeRectangle{0, 0, 1, 1}, false},
		{&shapeRectangle{0, 1, 1, 1}, &shapeRectangle{0, 0, 1, 1}, false},
		{&shapeRectangle{-1, 0, 1, 1}, &shapeRectangle{0, 0, 1, 1}, false},
		{&shapeRectangle{0, -1, 1, 1}, &shapeRectangle{0, 0, 1, 1}, false},
		{&shapeRectangle{0, 0, 3, 2}, &shapeRectangle{0, 0, 1, 1}, true},
		{&shapeRectangle{0, 0, 3, 2}, &shapeRectangle{2, 1, 1, 1}, true},
		{&shapeRectangle{0, 0, 3, 2}, &shapeRectangle{3, 1, 1, 1}, false},
		{&shapeRectangle{0, 0, 3, 2}, &shapeRectangle{2, 2, 1, 1}, false},
		{&shapeRectangle{0, 0, 3, 2}, &shapeRectangle{0, 1, 1, 1}, true},
		{&shapeRectangle{0, 0, 3, 2}, &shapeRectangle{2, 0, 1, 1}, true},
		{&shapeRectangle{0, 0, 3, 2}, &shapeRectangle{1, 1, 1, 1}, true},
		{&shapeRectangle{0, 0, 3, 2}, &shapeRectangle{2, 0, 1, 1}, true},
		{&shapeRectangle{0, 0, 3, 2}, &shapeRectangle{2, 1, 3, 3}, true},
		{&shapeRectangle{0, 0, 3, 2}, &shapeRectangle{-2, -2, 3, 3}, true},
		{&shapeRectangle{0, 0, 3, 2}, &shapeRectangle{-2, -3, 3, 3}, false},
		{&shapeRectangle{0, 0, 3, 2}, &shapeRectangle{-3, -2, 3, 3}, false},
	} {
		t.Run(fmt.Sprintf(`%#v %#v %#v`, tc.R, tc.S, tc.C), func(t *testing.T) {
			c := tc.R.Collides(tc.S)
			if c != tc.C {
				t.Error(c)
			}
			if c != tc.S.Collides(tc.R) {
				t.Error(c)
			}
			if c != tc.R.Collides(&shapeIsUnknown{tc.S}) {
				t.Error(c)
			}
			if c != (tc.R).Collides(&shapeHasUnknown{tc.S}) {
				t.Error(c)
			}
		})
	}
}

type shapeIsUnknown struct{ Shape }

type shapeHasUnknown struct{ Shape }

func (s *shapeHasUnknown) Collides(shape Shape) bool { return shape.Collides(s) }

func TestShapeRectangle_SetPosition(t *testing.T) {
	var s shapeRectangle
	s.SetPosition(2, 3)
	if s != (shapeRectangle{X: 2, Y: 3}) {
		t.Error(s)
	}
}

func TestShapeRectangle_Size(t *testing.T) {
	if w, h := (&shapeRectangle{1, 2, 3, 4}).Size(); w != 3 || h != 4 {
		t.Error(w, h)
	}
}

func TestShapeRectangle_Clone(t *testing.T) {
	a := &shapeRectangle{1, 2, 3, 4}
	if b := a.Clone().(*shapeRectangle); a == b || *a != *b || *b != (shapeRectangle{1, 2, 3, 4}) {
		t.Error(b)
	}
}
