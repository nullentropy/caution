package caution

import "testing"

func TestAddingANodeToANewParentTakesItFromTheOld(t *testing.T) {
	a, b, c := Panel(), Panel(), Panel()
	a.Add(c)
	b.Add(c)
	a.Clear()
	if len(a.Children()) != 0 || len(b.Children()) != 1 || c.Parent() != b {
		t.Fatalf("a has %d, b has %d, c's parent is b: %v", len(a.Children()), len(b.Children()), c.Parent() == b)
	}
}
