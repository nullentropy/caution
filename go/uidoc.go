package caution

import (
	"encoding/json"
	"fmt"
)

// A UI document ("nib") is a widget tree as data
type nibNode struct {
	Type string         `json:"type"`
	Name string         `json:"name,omitempty"`
	P    map[string]any `json:"p,omitempty"`
	Kids []nibNode      `json:"kids,omitempty"`
}

type nibDoc struct {
	// Format version, for future migrations.
	Caution int      `json:"caution"`
	Root    *nibNode `json:"root"`
}

// UI is a loaded document: the built tree plus name -> node outlets.
type UI struct {
	Root  *Node
	names map[string]*Node
}

func LoadUI(data []byte) (*UI, error) {
	var doc nibDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("caution: parsing ui document: %w", err)
	}
	if doc.Root == nil {
		return nil, fmt.Errorf("caution: ui document has no root")
	}
	u := &UI{names: map[string]*Node{}}
	root, err := u.build(doc.Root)
	if err != nil {
		return nil, err
	}
	u.Root = root
	return u, nil
}

// MustLoadUI is LoadUI for embedded documents, where a parse error is a
// build defect: it panics.
func MustLoadUI(data []byte) *UI {
	u, err := LoadUI(data)
	if err != nil {
		panic(err)
	}
	return u
}

// Node returns the named outlet. A missing name panics, so a document that no
// longer matches the code fails at mount rather than at first event.
func (u *UI) Node(name string) *Node {
	n := u.names[name]
	if n == nil {
		panic(fmt.Sprintf("caution: ui document has no node named %q", name))
	}
	return n
}

// SaveUI serializes a widget tree back to a UI document. Runtime-only props
// are stripped ("on" subscriptions, the "outline" tooling adorner) and "name"
// is hoisted to the outlet field.
func SaveUI(root *Node) ([]byte, error) {
	if root == nil {
		return nil, fmt.Errorf("caution: SaveUI: nil root")
	}
	doc := nibDoc{Caution: 1, Root: exportNib(root)}
	return json.MarshalIndent(&doc, "", "  ")
}

func exportNib(n *Node) *nibNode {
	nn := &nibNode{Type: n.typ}
	for k, v := range n.props {
		switch k {
		case "on", "outline":
			continue
		case "name":
			if s, ok := v.(string); ok && s != "" {
				nn.Name = s
			}
			continue
		}
		if nn.P == nil {
			nn.P = map[string]any{}
		}
		nn.P[k] = v
	}
	for _, kid := range n.kids {
		nn.Kids = append(nn.Kids, *exportNib(kid))
	}
	return nn
}

func (u *UI) build(nn *nibNode) (*Node, error) {
	if nn.Type == "" {
		return nil, fmt.Errorf("caution: ui node missing type")
	}
	n := newNode(nn.Type)
	for k, v := range nn.P {
		// Event subscriptions come from code bindings (OnClick and friends),
		// never from documents.
		if k == "on" {
			continue
		}
		n.props[k] = v
	}
	if nn.Name != "" {
		if _, dup := u.names[nn.Name]; dup {
			return nil, fmt.Errorf("caution: duplicate node name %q", nn.Name)
		}
		u.names[nn.Name] = n
		// Keep the name on the node so tooling can read and edit it and SaveUI
		// can round-trip it. The client ignores unknown props.
		n.props["name"] = nn.Name
	}
	for i := range nn.Kids {
		kid, err := u.build(&nn.Kids[i])
		if err != nil {
			return nil, err
		}
		n.Add(kid)
	}
	return n, nil
}
