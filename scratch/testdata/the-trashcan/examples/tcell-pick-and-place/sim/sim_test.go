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

func Test_spriteModel_distance(t *testing.T) {
	var (
		n = func(x, y, w, h int32) *spriteModel {
			return &spriteModel{
				Width:  w,
				Height: h,
				Shape:  NewSpriteShape(x, y, w, h),
			}
		}
		s = func(v *spriteModel) string {
			x, y := v.Shape.Position()
			return fmt.Sprintf(`(x=%d y=%d w=%d h=%d)`, x, y, v.Width, v.Height)
		}
	)
	for _, tc := range []struct {
		A, B *spriteModel
		V    float64
	}{
		{
			n(0, 0, 1, 1),
			n(0, 0, 1, 1),
			0,
		},
		{
			n(1, 0, 1, 1),
			n(0, 0, 1, 1),
			1,
		},
		{
			n(0, 1, 1, 1),
			n(0, 0, 1, 1),
			1,
		},
		{
			n(5, 5, 1, 1),
			n(5, 5, 1, 1),
			0,
		},
		{
			n(5, 5, 1, 1),
			n(9, 5, 1, 1),
			4,
		},
		{
			n(5, 5, 1, 1),
			n(5, 9, 1, 1),
			4,
		},
		{
			n(5, 5, 1, 1),
			n(6, 6, 1, 1),
			1.4142135623730951,
		},
		{
			n(5, 5, 1, 1),
			n(15, 15, 1, 1),
			1.4142135623730951 * 10.0,
		},
		{
			n(5, 5, 3, 3),
			n(15, 15, 1, 1),
			1.4142135623730951 * 8.0,
		},
		{
			n(5, 5, 1, 1),
			n(15, 15, 3, 3),
			1.4142135623730951 * 10.0,
		},
		{
			n(5, 5, 3, 4),
			n(15, 15, 1, 1),
			1.4142135623730951 * 7.5166481891864542,
		},
		{
			n(5, 5, 4, 3),
			n(15, 15, 1, 1),
			1.4142135623730951 * 7.5166481891864542,
		},
		{
			n(5, 5, 4, 1),
			n(8, 5, 1, 1),
			0,
		},
		{
			n(5, 5, 5, 1),
			n(8, 5, 1, 1),
			0,
		},
		{
			n(5, 5, 1, 4),
			n(5, 8, 1, 1),
			0,
		},
		{
			n(5, 5, 1, 5),
			n(5, 8, 1, 1),
			0,
		},
		{
			n(5, 5, 10, 10),
			n(8, 8, 1, 1),
			0,
		},
		{
			n(0, 0, 3, 4),
			n(2, 6, 3, 2),
			3,
		},
	} {
		t.Run(fmt.Sprintf(`%s_%s`, s(tc.A), s(tc.B)), func(t *testing.T) {
			v := tc.A.distance(tc.B)
			if v2 := tc.B.distance(tc.A); v != v2 {
				t.Error(v, v2)
			}
			if v != tc.V {
				t.Error(v)
			}
		})
	}
}
