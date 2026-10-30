package ui

import (
	"fmt"
	"slices"
	"testing"

	"github.com/nullentropy/caution/terminal/gfx"
)

const (
	cullRows = 300
	cullRowH = 40
	cullW    = 400
	cullH    = 300
)

// one scroll view holding far more rows than fit
func cullScene() (*UI, *ScrollView, []*Panel) {
	u := fakeUi()
	sv := NewScrollView()
	rows := make([]*Panel, cullRows)
	for i := range rows {
		row := NewPanel()
		row.Frame = &gfx.Rect{X: 0, Y: float32(i * cullRowH), W: cullW, H: cullRowH}
		label := NewLabel(fmt.Sprintf("row %d", i), gfx.NewFont(13, gfx.FontOpts{}), nil)
		label.Frame = &gfx.Rect{X: 8, Y: 10, W: 200, H: 18}
		row.Add(label)
		sv.Add(row)
		rows[i] = row
	}
	u.Root = sv
	return u, sv, rows
}

func count(dl *gfx.DisplayList, kind gfx.CmdKind) int {
	n := 0
	for _, c := range dl.Cmds {
		if c.Kind == kind {
			n++
		}
	}
	return n
}

func frame(u *UI) *gfx.DisplayList {
	dl := &gfx.DisplayList{}
	u.BuildFrame(dl, cullW, cullH)
	return dl
}

// visible rows plus the margin on each side, nowhere near the whole list
const maxPaintedRows = cullH/cullRowH + 2*cullMargin/cullRowH + 2

func TestScrollViewPaintsOnlyVisibleRows(t *testing.T) {
	u, _, _ := cullScene()
	if n := count(frame(u), gfx.CmdText); n == 0 || n > maxPaintedRows {
		t.Fatalf("painted %d labels of %d rows, want at most %d", n, cullRows, maxPaintedRows)
	}
}

// a structural insert fades every new row through its own offscreen layer, so
// painting rows nobody can see allocated one texture each
func TestInsertFadesOnlyVisibleRows(t *testing.T) {
	u, sv, _ := cullScene()
	rows := sv.Kids
	sv.Kids = nil
	frame(u)
	sv.Kids = rows
	u.NoteStructural()
	if n := count(frame(u), gfx.CmdLayerBegin); n == 0 || n > maxPaintedRows {
		t.Fatalf("faded %d rows of %d, want at most %d", n, cullRows, maxPaintedRows)
	}
}

// splicing must never replay a culled row's empty range once it is visible,
// so each scrolled frame has to match a from-scratch rebuild of the same state
func TestScrolledSpliceMatchesRebuild(t *testing.T) {
	u, sv, _ := cullScene()
	frame(u)
	for _, dy := range []float32{1000, 37, -400, 5000, -5000} {
		sv.ScrollBy(dy)
		spliced := frame(u)
		u.Invalidate()
		rebuilt := frame(u)
		if !slices.EqualFunc(spliced.Cmds, rebuilt.Cmds, cmdEqual) {
			t.Fatalf("after scrolling %v: spliced %d cmds, rebuilt %d, contents differ",
				dy, len(spliced.Cmds), len(rebuilt.Cmds))
		}
		if count(spliced, gfx.CmdText) == 0 {
			t.Fatalf("after scrolling %v nothing is painted", dy)
		}
	}
}

// reveal used to be consumed during paint, which never reaches a culled row
func TestRevealReachesCulledRow(t *testing.T) {
	u, sv, rows := cullScene()
	frame(u)
	rows[200].RequestReveal()
	frame(u)
	if sv.ScrollY == 0 {
		t.Fatal("revealing an offscreen row did not scroll")
	}
	frame(u)
	if b := rows[200].Bounds; !gfx.Overlaps(b, sv.Bounds) {
		t.Fatalf("revealed row at %v is outside the viewport %v", b, sv.Bounds)
	}
}

func cmdEqual(a, b gfx.Cmd) bool { return fmt.Sprint(a) == fmt.Sprint(b) }
