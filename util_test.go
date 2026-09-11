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
		n1     = new(node[Condition])
		n1n1   = new(node[Condition])
		n1n2   = &node[Condition]{node: func() (bt.Tick, []bt.Node) { panic(`unexpected call`) }}
		n1n3   = new(node[Condition])
		n1n3n1 = new(node[Condition])
		n1n4   = new(node[Condition])
		n2     = new(node[Condition])
		n2n1   = new(node[Condition])
		n2n2   = new(node[Condition])
		n3     = new(node[Condition])
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
	(&node[Condition]{node: func() (bt.Tick, []bt.Node) { panic(`unexpected call`) }}).append(nil)
	t.Error(`expected panic`)
}

func Test_node_copy(t *testing.T) {
	src := &node[Condition]{
		typ:    NodeTypeGoalRoot,
		status: &NodeStatus{},
	}
	src.status.SetTickCount(3)
	src.status.SetLastStatus(bt.Success)
	dst := new(node[Condition]).copy(src)

	if dst.typ != NodeTypeGoalRoot {
		t.Errorf("dst.typ = %v, want NodeTypeGoalRoot", dst.typ)
	}
	if dst.status == nil {
		t.Fatal("dst.status is nil")
	}
	if dst.status.TickCount() != 3 {
		t.Errorf("dst.status.TickCount() = %d, want 3", dst.status.TickCount())
	}
	if dst.status.LastStatus() != bt.Success {
		t.Errorf("dst.status.LastStatus() = %v, want Success", dst.status.LastStatus())
	}
}

func TestPpaConflicts(t *testing.T) {
	// Direct unit test for the conflicts() helper which previously had 0% coverage.
	// Construct PPAs manually with effects vs conditions.

	condKey := "x"

	trueCond := &simpleCondition{key: condKey, value: "desired"}
	falseEffect := &simpleEffect{key: condKey, value: "other"}
	trueEffect := &simpleEffect{key: condKey, value: "desired"}
	unrelatedEffect := &simpleEffect{key: "y", value: "anything"}

	// Helper to build a p(P) with a single condition and an o with single effect.
	buildP := func(cond Condition) *ppa[Condition] {
		root := &node[Condition]{typ: NodeTypePPARoot}
		p := &ppa[Condition]{root: root}
		root.ppa = p
		if cond != nil {
			leaf := &node[Condition]{typ: NodeTypePreconditionLeaf, precondition: &precondition[Condition]{condition: cond}}
			leaf.precondition.root = leaf
			andMap := map[any]*precondition[Condition]{cond.Key(): leaf.precondition}
			or := &preconditions[Condition]{and: andMap}
			act := &action[Condition]{or: []*preconditions[Condition]{or}, effects: map[any]Effect{}}
			p.actions = []*action[Condition]{act}
		}
		return p
	}
	buildO := func(eff Effect) *ppa[Condition] {
		root := &node[Condition]{typ: NodeTypePPARoot}
		o := &ppa[Condition]{root: root}
		root.ppa = o
		act := &action[Condition]{effects: map[any]Effect{eff.Key(): eff}}
		o.actions = []*action[Condition]{act}
		return o
	}

	// No conditions -> fast path false.
	t.Run("no conditions no conflict", func(t *testing.T) {
		p := buildP(nil)
		// explicitly empty actions to ensure pairs empty
		p.actions = []*action[Condition]{}
		o := buildO(falseEffect)
		if p.conflicts(o) {
			t.Fatalf("expected no conflict when p has no conditions")
		}
	})

	// Effect key matches condition key but value differs -> conflict true.
	t.Run("conflicting effect", func(t *testing.T) {
		p := buildP(trueCond)
		o := buildO(falseEffect)
		if !p.conflicts(o) {
			t.Fatalf("expected conflict: condition expects desired, effect is other")
		}
	})

	// Effect value matches condition -> no conflict.
	t.Run("matching effect no conflict", func(t *testing.T) {
		p := buildP(trueCond)
		o := buildO(trueEffect)
		if p.conflicts(o) {
			t.Fatalf("expected no conflict when effect matches condition")
		}
	})

	// Unrelated key -> no conflict.
	t.Run("unrelated key no conflict", func(t *testing.T) {
		p := buildP(trueCond)
		o := buildO(unrelatedEffect)
		if p.conflicts(o) {
			t.Fatalf("expected no conflict for unrelated keys")
		}
	})

	// Nested PPA via or.and queue traversal: o's action has a precondition whose
	// root is an expanded PPA. The conflicts check should queue that nested PPA.
	t.Run("nested ppa", func(t *testing.T) {
		p := buildP(trueCond)
		// Build nested PPA that itself conflicts.
		nestedRoot := &node[Condition]{typ: NodeTypePPARoot}
		nested := &ppa[Condition]{root: nestedRoot}
		nestedRoot.ppa = nested
		nested.actions = []*action[Condition]{{effects: map[any]Effect{condKey: falseEffect}}}

		// Outer O's action contains an 'or' that points to nestedRoot.
		nestedLeaf := &node[Condition]{typ: NodeTypePreconditionLeaf, precondition: &precondition[Condition]{condition: &simpleCondition{key: "z", value: "zv"}}}
		nestedLeaf.precondition.root = nestedLeaf
		// Mark this leaf as an expanded PPA root: its ppa root equals itself.
		// Actually for queue traversal, the condition is: and.root == and.root.ppa.root
		// So we need and.root.ppa.root == and.root
		leafPpaRoot := &node[Condition]{typ: NodeTypePPARoot}
		leafPpa := &ppa[Condition]{root: leafPpaRoot}
		leafPpaRoot.ppa = leafPpa
		leafPpa.actions = []*action[Condition]{{effects: map[any]Effect{condKey: falseEffect}}}
		nestedLeaf.precondition.root = leafPpaRoot
		nestedLeaf.ppa = leafPpa // not used but ensure consistency
		andMapNested := map[any]*precondition[Condition]{"z": nestedLeaf.precondition}
		orNested := &preconditions[Condition]{and: andMapNested}
		outerAct := &action[Condition]{
			effects: map[any]Effect{"unrelated": unrelatedEffect},
			or:      []*preconditions[Condition]{orNested},
		}
		outerRoot := &node[Condition]{typ: NodeTypePPARoot}
		outer := &ppa[Condition]{root: outerRoot}
		outerRoot.ppa = outer
		outer.actions = []*action[Condition]{outerAct}
		// This should find conflict via nested queue.
		if !p.conflicts(outer) {
			t.Fatalf("expected conflict via nested ppa traversal")
		}
	})
}

func TestPpaResolve_ReordersOnConflict(t *testing.T) {
	condKey := "x"
	cond := &simpleCondition{key: condKey, value: "desired"}
	conflictingEffect := &simpleEffect{key: condKey, value: "other"}

	// Build two PPA roots as children of a sequence parent.
	parent := &node[Condition]{tick: bt.Sequence, typ: NodeTypeActionRoot}

	oRoot := &node[Condition]{typ: NodeTypePPARoot}
	o := &ppa[Condition]{root: oRoot}
	oRoot.ppa = o
	oAct := &action[Condition]{effects: map[any]Effect{condKey: conflictingEffect}}
	o.actions = []*action[Condition]{oAct}

	pRoot := &node[Condition]{typ: NodeTypePPARoot}
	p := &ppa[Condition]{root: pRoot}
	pRoot.ppa = p
	leaf := &node[Condition]{typ: NodeTypePreconditionLeaf, precondition: &precondition[Condition]{condition: cond}}
	leaf.precondition.root = leaf
	andMap := map[any]*precondition[Condition]{condKey: leaf.precondition}
	or := &preconditions[Condition]{and: andMap}
	pAct := &action[Condition]{or: []*preconditions[Condition]{or}, effects: map[any]Effect{}}
	p.actions = []*action[Condition]{pAct}

	parent.append(nil, oRoot, pRoot)

	if parent.first != oRoot || parent.last != pRoot || pRoot.prev != oRoot {
		t.Fatalf("initial order wrong: first=%p oRoot=%p last=%p pRoot=%p prev=%p", parent.first, oRoot, parent.last, pRoot, pRoot.prev)
	}

	moved := p.resolve()
	if moved != 1 {
		t.Fatalf("resolve conflicts = %d want 1", moved)
	}
	// After resolve, pRoot should have been moved left of oRoot.
	if parent.first != pRoot || parent.last != oRoot {
		t.Fatalf("after resolve order wrong: first=%p want pRoot=%p last=%p want oRoot=%p prev checks pRoot.next=%p oRoot.prev=%p", parent.first, pRoot, parent.last, oRoot, pRoot.next, oRoot.prev)
	}
	if pRoot.next != oRoot || oRoot.prev != pRoot {
		t.Fatalf("linkage after resolve broken: pRoot.next=%p oRoot.prev=%p", pRoot.next, oRoot.prev)
	}

	// Second call should find no further conflict and return 0.
	if moved2 := p.resolve(); moved2 != 0 {
		t.Fatalf("second resolve = %d want 0", moved2)
	}
}

func TestPpaConflict_NoConflictReturnsNil(t *testing.T) {
	// Use planner integration: construct a state where no conflict exists, verify conflict() returns nil via resolve==0.
	// Minimal case: single action with matching condition.
	condKey := "k"
	cond := &simpleCondition{key: condKey, value: "v"}
	eff := &simpleEffect{key: condKey, value: "v"}
	parent := &node[Condition]{tick: bt.Sequence, typ: NodeTypeActionRoot}
	oRoot := &node[Condition]{typ: NodeTypePPARoot}
	o := &ppa[Condition]{root: oRoot}
	oRoot.ppa = o
	o.actions = []*action[Condition]{{effects: map[any]Effect{condKey: eff}}}
	pRoot := &node[Condition]{typ: NodeTypePPARoot}
	p := &ppa[Condition]{root: pRoot}
	pRoot.ppa = p
	leaf := &node[Condition]{typ: NodeTypePreconditionLeaf, precondition: &precondition[Condition]{condition: cond}}
	leaf.precondition.root = leaf
	andMap := map[any]*precondition[Condition]{condKey: leaf.precondition}
	p.actions = []*action[Condition]{{or: []*preconditions[Condition]{{and: andMap}}}}
	parent.append(nil, oRoot, pRoot)
	if p.conflict() != nil {
		t.Fatalf("expected no conflict, got %v", p.conflict())
	}
	if p.resolve() != 0 {
		t.Fatalf("resolve should be 0 when no conflict")
	}
}
