package proto

import (
	"testing"

	"github.com/nullentropy/caution/go/terminal/ui"
)

type nullSink struct{}

func (nullSink) Event(int, string, any) {}

// The local-echo contract, at the inflate seam: a focused field ignores
// plain value sets, and a fresh overrideSeq forces one through.
func TestFocusedFieldValueSetsHonorLocalEchoAndOverride(t *testing.T) {
	ns := NewNodeStore(nullSink{})
	w := ns.Build(nodeJSON{ID: 1, Type: "textfield", P: props{"value": "draft"}})
	tf := w.(*ui.TextField)
	tf.Focused = true // the user is editing

	ns.Apply(tf, 1, props{"value": "server"})
	if tf.Value != "draft" {
		t.Fatalf("focused field took a plain value set: %q", tf.Value)
	}

	ns.Apply(tf, 1, props{"value": "$1,000", "overrideSeq": float64(1)})
	if tf.Value != "$1,000" {
		t.Fatalf("override did not apply: %q", tf.Value)
	}

	// The same seq must not re-force (patches can repeat props on remount).
	ns.Apply(tf, 1, props{"value": "stale", "overrideSeq": float64(1)})
	if tf.Value != "$1,000" {
		t.Fatalf("stale overrideSeq re-forced: %q", tf.Value)
	}
}

func TestTextareaSharesTheOverrideContract(t *testing.T) {
	ns := NewNodeStore(nullSink{})
	w := ns.Build(nodeJSON{ID: 1, Type: "textarea", P: props{"value": "a\nb"}})
	ta := w.(*ui.TextArea)
	ta.Focused = true

	ns.Apply(ta, 1, props{"value": "x"})
	if ta.Value != "a\nb" {
		t.Fatalf("focused textarea took a plain value set: %q", ta.Value)
	}
	ns.Apply(ta, 1, props{"value": "x\ny", "overrideSeq": float64(1)})
	if ta.Value != "x\ny" {
		t.Fatalf("override did not apply: %q", ta.Value)
	}
}
