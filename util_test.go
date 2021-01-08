/*
   Copyright 2021 Joseph Cumines

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package pabt

import (
	"fmt"
	bt "github.com/joeycumines/go-behaviortree"
	"testing"
)

func Test_node_append(t *testing.T) {
	var (
		n1     = new(node)
		n1n1   = new(node)
		n1n2   = &node{node: func() (bt.Tick, []bt.Node) { panic(`unexpected call`) }}
		n1n3   = new(node)
		n1n3n1 = new(node)
		n1n4   = new(node)
		n2     = new(node)
		n2n1   = new(node)
		n2n2   = new(node)
		n3     = new(node)
	)
	t.Logf(
		"\nn1 = %p\nn1n1 = %p\nn1n2 = %p\nn1n3 = %p\nn1n3n1 = %p\nn1n4 = %p\nn2 = %p\nn2n1 = %p\nn2n2 = %p\nn3 = %p",
		n1,
		n1n1,
		n1n2,
		n1n3,
		n1n3n1,
		n1n4,
		n2,
		n2n1,
		n2n2,
		n3,
	)
	n1.append(nil, n1n1)
	if n := n1; n.parent != nil || n.first != n1n1 || n.last != n1n1 || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n1; n.parent != n1 || n.first != nil || n.last != nil || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	n1.append(nil, n1n2)
	if n := n1; n.parent != nil || n.first != n1n1 || n.last != n1n2 || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n1; n.parent != n1 || n.first != nil || n.last != nil || n.prev != nil || n.next != n1n2 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n2; n.parent != n1 || n.first != nil || n.last != nil || n.prev != n1n1 || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	n1.append(nil, n1n3)
	if n := n1; n.parent != nil || n.first != n1n1 || n.last != n1n3 || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n1; n.parent != n1 || n.first != nil || n.last != nil || n.prev != nil || n.next != n1n2 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n2; n.parent != n1 || n.first != nil || n.last != nil || n.prev != n1n1 || n.next != n1n3 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n3; n.parent != n1 || n.first != nil || n.last != nil || n.prev != n1n2 || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	n1.append(nil, n1n4)
	if n := n1; n.parent != nil || n.first != n1n1 || n.last != n1n4 || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n1; n.parent != n1 || n.first != nil || n.last != nil || n.prev != nil || n.next != n1n2 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n2; n.parent != n1 || n.first != nil || n.last != nil || n.prev != n1n1 || n.next != n1n3 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n3; n.parent != n1 || n.first != nil || n.last != nil || n.prev != n1n2 || n.next != n1n4 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n4; n.parent != n1 || n.first != nil || n.last != nil || n.prev != n1n3 || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	n1n3.append(nil, n1n3n1)
	if n := n1; n.parent != nil || n.first != n1n1 || n.last != n1n4 || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n1; n.parent != n1 || n.first != nil || n.last != nil || n.prev != nil || n.next != n1n2 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n2; n.parent != n1 || n.first != nil || n.last != nil || n.prev != n1n1 || n.next != n1n3 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n3; n.parent != n1 || n.first != n1n3n1 || n.last != n1n3n1 || n.prev != n1n2 || n.next != n1n4 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n3n1; n.parent != n1n3 || n.first != nil || n.last != nil || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n4; n.parent != n1 || n.first != nil || n.last != nil || n.prev != n1n3 || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	n2.append(nil, n2n1)
	n2.append(nil, n2n2)
	n3.append(nil, n1)
	n3.append(nil, n2)
	if n := n1; n.parent != n3 || n.first != n1n1 || n.last != n1n4 || n.prev != nil || n.next != n2 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n1; n.parent != n1 || n.first != nil || n.last != nil || n.prev != nil || n.next != n1n2 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n2; n.parent != n1 || n.first != nil || n.last != nil || n.prev != n1n1 || n.next != n1n3 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n3; n.parent != n1 || n.first != n1n3n1 || n.last != n1n3n1 || n.prev != n1n2 || n.next != n1n4 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n3n1; n.parent != n1n3 || n.first != nil || n.last != nil || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n4; n.parent != n1 || n.first != nil || n.last != nil || n.prev != n1n3 || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n2; n.parent != n3 || n.first != n2n1 || n.last != n2n2 || n.prev != n1 || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n3; n.parent != nil || n.first != n1 || n.last != n2 || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	n3.delete()
	if n := n3; n.parent != nil || n.first != n1 || n.last != n2 || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	n1n3n1.append(nil, n2)
	if n := n1; n.parent != n3 || n.first != n1n1 || n.last != n1n4 || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n1; n.parent != n1 || n.first != nil || n.last != nil || n.prev != nil || n.next != n1n2 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n2; n.parent != n1 || n.first != nil || n.last != nil || n.prev != n1n1 || n.next != n1n3 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n3; n.parent != n1 || n.first != n1n3n1 || n.last != n1n3n1 || n.prev != n1n2 || n.next != n1n4 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n3n1; n.parent != n1n3 || n.first != n2 || n.last != n2 || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n4; n.parent != n1 || n.first != nil || n.last != nil || n.prev != n1n3 || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n2; n.parent != n1n3n1 || n.first != n2n1 || n.last != n2n2 || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n3; n.parent != nil || n.first != n1 || n.last != n1 || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	n1n3.delete()
	if n := n1; n.parent != n3 || n.first != n1n1 || n.last != n1n4 || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n1; n.parent != n1 || n.first != nil || n.last != nil || n.prev != nil || n.next != n1n2 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n2; n.parent != n1 || n.first != nil || n.last != nil || n.prev != n1n1 || n.next != n1n4 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n3; n.parent != nil || n.first != n1n3n1 || n.last != n1n3n1 || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n3n1; n.parent != n1n3 || n.first != n2 || n.last != n2 || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n4; n.parent != n1 || n.first != nil || n.last != nil || n.prev != n1n2 || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n2; n.parent != n1n3n1 || n.first != n2n1 || n.last != n2n2 || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n3; n.parent != nil || n.first != n1 || n.last != n1 || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	n1n1.delete()
	if n := n1; n.parent != n3 || n.first != n1n2 || n.last != n1n4 || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n1; n.parent != nil || n.first != nil || n.last != nil || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n2; n.parent != n1 || n.first != nil || n.last != nil || n.prev != nil || n.next != n1n4 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n3; n.parent != nil || n.first != n1n3n1 || n.last != n1n3n1 || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n3n1; n.parent != n1n3 || n.first != n2 || n.last != n2 || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n4; n.parent != n1 || n.first != nil || n.last != nil || n.prev != n1n2 || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n2; n.parent != n1n3n1 || n.first != n2n1 || n.last != n2n2 || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n3; n.parent != nil || n.first != n1 || n.last != n1 || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	n1.append(n1n2, n1n1)
	if n := n1; n.parent != n3 || n.first != n1n1 || n.last != n1n4 || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n1; n.parent != n1 || n.first != nil || n.last != nil || n.prev != nil || n.next != n1n2 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n2; n.parent != n1 || n.first != nil || n.last != nil || n.prev != n1n1 || n.next != n1n4 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n3; n.parent != nil || n.first != n1n3n1 || n.last != n1n3n1 || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n3n1; n.parent != n1n3 || n.first != n2 || n.last != n2 || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n4; n.parent != n1 || n.first != nil || n.last != nil || n.prev != n1n2 || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n2; n.parent != n1n3n1 || n.first != n2n1 || n.last != n2n2 || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n3; n.parent != nil || n.first != n1 || n.last != n1 || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	n1.append(n1n1, n1n2)
	if n := n1; n.parent != n3 || n.first != n1n2 || n.last != n1n4 || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n1; n.parent != n1 || n.first != nil || n.last != nil || n.prev != n1n2 || n.next != n1n4 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n2; n.parent != n1 || n.first != nil || n.last != nil || n.prev != nil || n.next != n1n1 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n3; n.parent != nil || n.first != n1n3n1 || n.last != n1n3n1 || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n3n1; n.parent != n1n3 || n.first != n2 || n.last != n2 || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n4; n.parent != n1 || n.first != nil || n.last != nil || n.prev != n1n1 || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n2; n.parent != n1n3n1 || n.first != n2n1 || n.last != n2n2 || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n3; n.parent != nil || n.first != n1 || n.last != n1 || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	n1.append(n1n1, n1n2)
	if n := n1; n.parent != n3 || n.first != n1n2 || n.last != n1n4 || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n1; n.parent != n1 || n.first != nil || n.last != nil || n.prev != n1n2 || n.next != n1n4 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n2; n.parent != n1 || n.first != nil || n.last != nil || n.prev != nil || n.next != n1n1 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n3; n.parent != nil || n.first != n1n3n1 || n.last != n1n3n1 || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n3n1; n.parent != n1n3 || n.first != n2 || n.last != n2 || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n4; n.parent != n1 || n.first != nil || n.last != nil || n.prev != n1n1 || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n2; n.parent != n1n3n1 || n.first != n2n1 || n.last != n2n2 || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n3; n.parent != nil || n.first != n1 || n.last != n1 || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	n3.append(nil, n2, n1)
	if n := n1; n.parent != n3 || n.first != n1n2 || n.last != n1n4 || n.prev != n2 || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n1; n.parent != n1 || n.first != nil || n.last != nil || n.prev != n1n2 || n.next != n1n4 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n2; n.parent != n1 || n.first != nil || n.last != nil || n.prev != nil || n.next != n1n1 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n3; n.parent != nil || n.first != n1n3n1 || n.last != n1n3n1 || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n3n1; n.parent != n1n3 || n.first != nil || n.last != nil || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n4; n.parent != n1 || n.first != nil || n.last != nil || n.prev != n1n1 || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n2; n.parent != n3 || n.first != n2n1 || n.last != n2n2 || n.prev != nil || n.next != n1 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n3; n.parent != nil || n.first != n2 || n.last != n1 || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}

	n2.append(
		n2n2,
		n1,
		n1n1,
		n1n2,
		n1n3,
		n1n3n1,
		n1n4,
		n2n1,
		n3,
	)
	if n := n1; n.parent != n2 || n.first != nil || n.last != nil || n.prev != nil || n.next != n1n1 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n1; n.parent != n2 || n.first != nil || n.last != nil || n.prev != n1 || n.next != n1n2 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n2; n.parent != n2 || n.first != nil || n.last != nil || n.prev != n1n1 || n.next != n1n3 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n3; n.parent != n2 || n.first != nil || n.last != nil || n.prev != n1n2 || n.next != n1n3n1 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n3n1; n.parent != n2 || n.first != nil || n.last != nil || n.prev != n1n3 || n.next != n1n4 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n4; n.parent != n2 || n.first != nil || n.last != nil || n.prev != n1n3n1 || n.next != n2n1 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n2; n.parent != n3 || n.first != n1 || n.last != n2n2 || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n2n1; n.parent != n2 || n.first != nil || n.last != nil || n.prev != n1n4 || n.next != n3 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n2n2; n.parent != n2 || n.first != nil || n.last != nil || n.prev != n3 || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n3; n.parent != n2 || n.first != n2 || n.last != n2 || n.prev != n2n1 || n.next != n2n2 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}

	n2.delete()
	if n := n1; n.parent != n2 || n.first != nil || n.last != nil || n.prev != nil || n.next != n1n1 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n1; n.parent != n2 || n.first != nil || n.last != nil || n.prev != n1 || n.next != n1n2 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n2; n.parent != n2 || n.first != nil || n.last != nil || n.prev != n1n1 || n.next != n1n3 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n3; n.parent != n2 || n.first != nil || n.last != nil || n.prev != n1n2 || n.next != n1n3n1 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n3n1; n.parent != n2 || n.first != nil || n.last != nil || n.prev != n1n3 || n.next != n1n4 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n1n4; n.parent != n2 || n.first != nil || n.last != nil || n.prev != n1n3n1 || n.next != n2n1 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n2; n.parent != nil || n.first != n1 || n.last != n2n2 || n.prev != nil || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n2n1; n.parent != n2 || n.first != nil || n.last != nil || n.prev != n1n4 || n.next != n3 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n2n2; n.parent != n2 || n.first != nil || n.last != nil || n.prev != n3 || n.next != nil {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
	if n := n3; n.parent != n2 || n.first != nil || n.last != nil || n.prev != n2n1 || n.next != n2n2 {
		t.Errorf("%p %p %p %p %p", n.parent, n.first, n.last, n.prev, n.next)
	}
}

func Test_node_append_panic(t *testing.T) {
	defer func() {
		if r := fmt.Sprint(recover()); r != `pabt: invalid append` {
			t.Error(r)
		}
	}()
	(&node{node: func() (bt.Tick, []bt.Node) { panic(`unexpected call`) }}).append(nil)
	t.Error(`expected panic`)
}
