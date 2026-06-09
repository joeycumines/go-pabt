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
	"strings"

	bt "github.com/joeycumines/go-behaviortree"
)

// Printer is a pabt-aware [bt.Printer] that enriches tree output with node type
// labels, post-condition info, and effects.
//
// It may be assigned to [bt.DefaultPrinter] to get pabt-aware tree output:
//
//	bt.DefaultPrinter = pabt.Printer
//
// The output format for each node is:
//
//	[frame1 frame2]  NodeType | function.name
//
// For leaf nodes with post-condition or effects info, additional meta is appended:
//
//	[frame1 frame2 post:condKey=condVal]  PreconditionLeaf | function.name
//	[frame1 frame2 effects:key=val,...]  ActionNode | function.name
var Printer bt.Printer = bt.TreePrinter{
	Inspector: pabtInspector,
	Formatter: bt.DefaultPrinterFormatter,
}

// pabtInspector is the Inspector function used by [Printer].
// It extends [bt.DefaultPrinterInspector] with pabt-specific metadata.
func pabtInspector(node bt.Node, tick bt.Tick) (meta []any, value any) {
	baseMeta, _ := bt.DefaultPrinterInspector(node, tick)

	// Use only the file:line entries from the default inspector (indices 1 and 3),
	// matching the pattern established by the existing test helper patchTreeMeta.
	if len(baseMeta) >= 4 {
		meta = []any{baseMeta[1], baseMeta[3]}
	} else {
		meta = []any{}
	}

	// Determine the node type label.
	nodeType := GetNodeType(node)
	typeLabel := nodeType.String()

	// Build the value string: NodeType | tickFunction
	tickName := "-"
	if f := tick.Frame(); f != nil && f.Function != "" {
		tickName = f.Function
	}

	// If the node has an explicit Name (set via bt.UseName), prefer it for the
	// type label, since pabt nodes already set Name to their NodeType string.
	if name := node.Name(); name != "" {
		typeLabel = name
	}

	value = typeLabel + " | " + tickName

	// Enrich meta with post-condition or effects info for relevant leaf nodes.
	if info := GetNodeInfo(node); info != nil {
		switch info.Type {
		case NodeTypePreconditionLeaf, NodeTypePPAPost:
			if info.Condition != nil {
				meta = append(meta, fmt.Sprintf("post:%s=%v", formatVariableKey(info.VariableKey), formatConditionMatch(info.Condition)))
			}
		case NodeTypeActionNode:
			if len(info.Effects) > 0 {
				meta = append(meta, "effects:"+formatEffects(info.Effects))
			}
		}
	}

	return
}

// formatVariableKey formats a variable key for display in the printer output.
func formatVariableKey(key any) string {
	if key == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%v", key)
}

// formatConditionMatch formats a Condition for display, showing its key and
// a representation of what it matches.
func formatConditionMatch(c Condition) string {
	if c == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%v", c)
}

// formatEffects formats an Effects slice into a compact comma-separated string.
func formatEffects(effects Effects) string {
	if len(effects) == 0 {
		return ""
	}
	parts := make([]string, len(effects))
	for i, e := range effects {
		if e == nil {
			parts[i] = "<nil>"
			continue
		}
		parts[i] = fmt.Sprintf("%s=%v", formatVariableKey(e.Key()), e.Value())
	}
	return strings.Join(parts, ",")
}
