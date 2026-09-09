package ui

import (
	"strings"
	"testing"

	"github.com/get-vix/vix/internal/protocol"
)

// forkRec builds a not-attached user record in /work with fork lineage and a
// creation timestamp (StartedAt) that drives tree ordering.
func forkRec(id, parent, started string) protocol.ThreadSummary {
	return protocol.ThreadSummary{ID: id, CWD: "/work", Title: "title-" + id, ParentID: parent, StartedAt: started}
}

// prefixByID maps each row's thread id to its computed tree prefix.
func prefixByID(m *Model) map[string]string {
	out := map[string]string{}
	for _, b := range m.userDirBlocks() {
		for _, r := range b.rows {
			if r.sum != nil {
				out[r.sum.ID] = r.treePrefix
			} else if r.liveIdx >= 0 {
				out[m.threads[r.liveIdx].daemonThreadID] = r.treePrefix
			}
		}
	}
	return out
}

// orderIDs returns the block-0 row ids in display order.
func orderIDs(m *Model) []string {
	var ids []string
	for _, r := range m.userDirBlocks()[0].rows {
		if r.sum != nil {
			ids = append(ids, r.sum.ID)
		} else if r.liveIdx >= 0 {
			ids = append(ids, m.threads[r.liveIdx].daemonThreadID)
		}
	}
	return ids
}

// TestForkTreeOrderPrefixes: a parent with one child, and that child with two
// children, produce a depth-first order and the box-drawing leads from the spec
// ("╰─ " for a last/only child, "├─ " for a non-last, with 3-cell indents).
func TestForkTreeOrderPrefixes(t *testing.T) {
	m := &Model{cwd: "/work", userThreadRecords: []protocol.ThreadSummary{
		forkRec("p", "", "2026-01-01T00:00:00Z"),
		forkRec("c", "p", "2026-01-02T00:00:00Z"),
		forkRec("g1", "c", "2026-01-03T00:00:00Z"),
		forkRec("g2", "c", "2026-01-04T00:00:00Z"),
	}}

	if got, want := orderIDs(m), []string{"p", "c", "g1", "g2"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", got, want)
	}
	pref := prefixByID(m)
	cases := map[string]string{
		"p":  "",
		"c":  "╰─ ",
		"g1": "   ├─ ",
		"g2": "   ╰─ ",
	}
	for id, want := range cases {
		if pref[id] != want {
			t.Errorf("prefix[%s] = %q, want %q", id, pref[id], want)
		}
	}
}

// TestForkTreeTwoTopChildrenVerticalBar: with two children of the root, the
// first is "├─ " and a grandchild under it continues with a vertical bar
// "│  " because a sibling still follows below.
func TestForkTreeVerticalContinuation(t *testing.T) {
	m := &Model{cwd: "/work", userThreadRecords: []protocol.ThreadSummary{
		forkRec("p", "", "2026-01-01T00:00:00Z"),
		forkRec("c1", "p", "2026-01-02T00:00:00Z"),
		forkRec("gc", "c1", "2026-01-03T00:00:00Z"),
		forkRec("c2", "p", "2026-01-04T00:00:00Z"),
	}}
	pref := prefixByID(m)
	if pref["c1"] != "├─ " {
		t.Errorf("c1 prefix = %q, want ├─ ", pref["c1"])
	}
	// gc is under the non-last child c1, so its indent keeps the bar.
	if pref["gc"] != "│  ╰─ " {
		t.Errorf("gc prefix = %q, want │  ╰─ ", pref["gc"])
	}
	if pref["c2"] != "╰─ " {
		t.Errorf("c2 prefix = %q, want ╰─ ", pref["c2"])
	}
}

// TestForkTreeOrphanIsRoot: a fork whose parent is not in the list renders as a
// root (empty prefix), never dropped.
func TestForkTreeOrphanIsRoot(t *testing.T) {
	m := &Model{cwd: "/work", userThreadRecords: []protocol.ThreadSummary{
		forkRec("orphan", "gone", "2026-01-02T00:00:00Z"),
	}}
	pref := prefixByID(m)
	if _, ok := pref["orphan"]; !ok {
		t.Fatal("orphan fork was dropped")
	}
	if pref["orphan"] != "" {
		t.Errorf("orphan prefix = %q, want empty (root)", pref["orphan"])
	}
}

// TestForkTreeMixedLiveAndRecord: a live parent with a persisted child record
// still nests the child under it.
func TestForkTreeMixedLiveAndRecord(t *testing.T) {
	parent := liveAt("/work", "2026-01-01T00:00:00Z")
	parent.daemonThreadID = "p"
	m := &Model{
		cwd:               "/work",
		threads:           []*ThreadState{parent},
		userThreadRecords: []protocol.ThreadSummary{forkRec("c", "p", "2026-01-02T00:00:00Z")},
	}
	if got, want := orderIDs(m), []string{"p", "c"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want [p c]", got)
	}
	if pref := prefixByID(m); pref["c"] != "╰─ " {
		t.Errorf("child prefix = %q, want ╰─ ", pref["c"])
	}
}

// TestGhostRowNotSelectableAndExcludedFromCount: a closed fork ancestor row is
// display-only — never selectable, and not counted in its dir header.
func TestGhostRowNotSelectableAndExcludedFromCount(t *testing.T) {
	ghost := forkRec("p", "", "2026-01-01T00:00:00Z")
	ghost.Closed = true
	m := &Model{cwd: "/work", userThreadRecords: []protocol.ThreadSummary{
		ghost,
		forkRec("c", "p", "2026-01-02T00:00:00Z"),
	}}

	var dirCount int
	var ghostRow, childRow *threadListRow
	lr := m.threadListRows()
	for i := range lr {
		r := lr[i]
		switch {
		case r.kind == rowDirHeader:
			dirCount = r.count
		case r.kind == rowUserThread && r.sum.ID == "p":
			ghostRow = &lr[i]
		case r.kind == rowUserThread && r.sum.ID == "c":
			childRow = &lr[i]
		}
	}
	if dirCount != 1 {
		t.Errorf("dir header count = %d, want 1 (ghost excluded)", dirCount)
	}
	if ghostRow == nil || !ghostRow.ghost || ghostRow.selectable() {
		t.Errorf("ghost row must be present, marked ghost, and not selectable: %+v", ghostRow)
	}
	if childRow == nil || !childRow.selectable() {
		t.Error("child row should be selectable")
	}
	// The selectable set skips the ghost entirely.
	for _, r := range m.selectableThreadRows() {
		if r.ghost {
			t.Error("selectableThreadRows must not include a ghost row")
		}
	}
}

// TestRenderThreadsViewForkTree: the rendered tab shows the box-drawing leads
// and a "(closed)" marker for a ghost ancestor.
func TestRenderThreadsViewForkTree(t *testing.T) {
	rows := []threadListRow{
		{kind: rowUserHeader},
		{kind: rowDirHeader, dir: "/work", count: 2},
		{kind: rowUserThread, liveIdx: -1, ghost: true, treePrefix: "", sum: protocol.ThreadSummary{ID: "parent", Title: "Root idea", Closed: true, StartedAt: "2026-01-01T00:00:00Z"}},
		{kind: rowUserThread, liveIdx: -1, treePrefix: "╰─ ", sum: protocol.ThreadSummary{ID: "childaa", Title: "Variant A", LastRequestAt: "2026-01-02T00:00:00Z"}},
	}
	out := renderThreadsView(rows, 120, 40, NewStyles(true), 0, "")
	for _, want := range []string{"Root idea", "(closed)", "╰─ ", "Variant A", "childaa"} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q\n---\n%s", want, out)
		}
	}
}

// TestOpenForkChildCount: the local pre-check counts open forks across live
// tabs and persisted records, deduping and ignoring closed ancestors.
func TestOpenForkChildCount(t *testing.T) {
	parent := liveAt("/work", "2026-01-01T00:00:00Z")
	parent.daemonThreadID = "p"
	closedChild := forkRec("closed", "p", "2026-01-02T00:00:00Z")
	closedChild.Closed = true
	m := &Model{
		cwd:     "/work",
		threads: []*ThreadState{parent},
		userThreadRecords: []protocol.ThreadSummary{
			forkRec("c", "p", "2026-01-03T00:00:00Z"),
			closedChild, // closed: must not count
			forkRec("unrelated", "", "2026-01-04T00:00:00Z"),
		},
	}
	if n := m.openForkChildCount("p"); n != 1 {
		t.Errorf("openForkChildCount = %d, want 1 (only the open fork)", n)
	}
	if n := m.openForkChildCount("nobody"); n != 0 {
		t.Errorf("openForkChildCount for a childless id = %d, want 0", n)
	}
}

func TestForkCloseBlockedMsgPlural(t *testing.T) {
	if s := forkCloseBlockedMsg(1); !strings.Contains(s, "1 open thread was") {
		t.Errorf("singular message wrong: %q", s)
	}
	if s := forkCloseBlockedMsg(3); !strings.Contains(s, "3 open threads were") {
		t.Errorf("plural message wrong: %q", s)
	}
}
