package daemon

import (
	"strings"
	"testing"
	"time"

	"github.com/get-vix/vix/internal/config"
	"github.com/get-vix/vix/internal/protocol"
)

// forkRecord builds a minimal open-thread record with the fork fields set.
func forkRecord(id, parentID string, startedAgo time.Duration) threadRecord {
	return threadRecord{
		ID:         id,
		CWD:        "/work",
		Model:      "anthropic/claude-x",
		ParentID:   parentID,
		ThreadMode: "chat",
		StartedAt:  time.Now().Add(-startedAgo).Truncate(time.Second),
	}
}

func TestSummaryCarriesForkFields(t *testing.T) {
	rec := forkRecord("child", "parent", time.Minute)
	rec.ForkTurnIdx = 3
	sum := rec.summary()
	if sum.ParentID != "parent" || sum.ForkTurnIdx != 3 {
		t.Fatalf("summary fork fields = %q/%d, want parent/3", sum.ParentID, sum.ForkTurnIdx)
	}
	if sum.Closed {
		t.Error("summary Closed should default false")
	}
}

func TestClosedForkAncestorsIncludesClosedParent(t *testing.T) {
	paths := testPaths(t)
	parent := forkRecord("parent", "", 2*time.Hour)
	child := forkRecord("child", "parent", time.Hour)
	// Parent is closed, child is open.
	if err := saveThreadRecord(paths, parent); err != nil {
		t.Fatal(err)
	}
	if err := moveThreadToClosed(paths, "parent"); err != nil {
		t.Fatal(err)
	}
	if err := saveThreadRecord(paths, child); err != nil {
		t.Fatal(err)
	}

	open := listOpenThreadRecords(paths)
	anc := closedForkAncestors(paths, open)
	if len(anc) != 1 || anc[0].ID != "parent" {
		t.Fatalf("closedForkAncestors = %+v, want [parent]", anc)
	}
}

func TestClosedForkAncestorsWalksChain(t *testing.T) {
	paths := testPaths(t)
	// grandparent(closed) <- parent(closed) <- child(open)
	for _, r := range []threadRecord{
		forkRecord("gp", "", 3*time.Hour),
		forkRecord("parent", "gp", 2*time.Hour),
	} {
		if err := saveThreadRecord(paths, r); err != nil {
			t.Fatal(err)
		}
		if err := moveThreadToClosed(paths, r.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := saveThreadRecord(paths, forkRecord("child", "parent", time.Hour)); err != nil {
		t.Fatal(err)
	}

	anc := closedForkAncestors(paths, listOpenThreadRecords(paths))
	if len(anc) != 2 {
		t.Fatalf("closedForkAncestors returned %d, want 2 (parent + gp): %+v", len(anc), anc)
	}
	// Creation order: gp (older) before parent.
	if anc[0].ID != "gp" || anc[1].ID != "parent" {
		t.Fatalf("ancestor order = %s,%s, want gp,parent", anc[0].ID, anc[1].ID)
	}
}

func TestClosedForkAncestorsStopsAtMissing(t *testing.T) {
	paths := testPaths(t)
	// child points at a parent that exists nowhere: no ancestor, no crash.
	if err := saveThreadRecord(paths, forkRecord("child", "ghost", time.Hour)); err != nil {
		t.Fatal(err)
	}
	if anc := closedForkAncestors(paths, listOpenThreadRecords(paths)); len(anc) != 0 {
		t.Fatalf("expected no ancestors for a missing parent, got %+v", anc)
	}
}

func TestClosedForkAncestorsNotDuplicatedForSiblings(t *testing.T) {
	paths := testPaths(t)
	parent := forkRecord("parent", "", 2*time.Hour)
	if err := saveThreadRecord(paths, parent); err != nil {
		t.Fatal(err)
	}
	if err := moveThreadToClosed(paths, "parent"); err != nil {
		t.Fatal(err)
	}
	// Two open children of the same closed parent.
	for _, id := range []string{"a", "b"} {
		if err := saveThreadRecord(paths, forkRecord(id, "parent", time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	anc := closedForkAncestors(paths, listOpenThreadRecords(paths))
	if len(anc) != 1 {
		t.Fatalf("closed parent listed %d times, want once: %+v", len(anc), anc)
	}
}

func TestOpenForkChildren(t *testing.T) {
	paths := testPaths(t)
	if err := saveThreadRecord(paths, forkRecord("parent", "", 2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := saveThreadRecord(paths, forkRecord("child", "parent", time.Hour)); err != nil {
		t.Fatal(err)
	}
	// unrelated thread must not count
	if err := saveThreadRecord(paths, forkRecord("other", "", time.Hour)); err != nil {
		t.Fatal(err)
	}

	kids := openForkChildren(paths, "parent", nil)
	if len(kids) != 1 || kids[0].ID != "child" {
		t.Fatalf("openForkChildren = %+v, want [child]", kids)
	}

	// A child whose close is in flight (skip) is treated as closed.
	skip := func(id string) bool { return id == "child" }
	if kids := openForkChildren(paths, "parent", skip); len(kids) != 0 {
		t.Fatalf("openForkChildren with skip should be empty, got %+v", kids)
	}
}

func TestThreadListSurfacesClosedForkAncestor(t *testing.T) {
	dir := t.TempDir()
	paths := config.NewVixPaths(dir, "", "/work")

	parent := forkRecord("parent", "", 2*time.Hour)
	if err := saveThreadRecord(paths, parent); err != nil {
		t.Fatal(err)
	}
	if err := moveThreadToClosed(paths, "parent"); err != nil {
		t.Fatal(err)
	}
	if err := saveThreadRecord(paths, forkRecord("child", "parent", time.Hour)); err != nil {
		t.Fatal(err)
	}

	srv := newInstanceTestServer(t)
	RegisterBuiltinHandlers(srv)
	resp, err := srv.GetHandler("thread.list")(map[string]any{"cwd": "/work", "config_dir": dir})
	if err != nil {
		t.Fatal(err)
	}
	sums, ok := resp["threads"].([]protocol.ThreadSummary)
	if !ok {
		t.Fatalf("threads has unexpected type %T", resp["threads"])
	}
	var parentSum *protocol.ThreadSummary
	for i := range sums {
		if sums[i].ID == "parent" {
			parentSum = &sums[i]
		}
	}
	if parentSum == nil {
		t.Fatal("closed parent not surfaced in thread.list despite having an open fork")
	}
	if !parentSum.Closed {
		t.Error("closed fork ancestor should be marked Closed in the summary")
	}
}

func TestThreadDismissRefusedWithOpenForks(t *testing.T) {
	dir := t.TempDir()
	paths := config.NewVixPaths(dir, "", "/work")
	if err := saveThreadRecord(paths, forkRecord("parent", "", 2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := saveThreadRecord(paths, forkRecord("child", "parent", time.Hour)); err != nil {
		t.Fatal(err)
	}

	srv := newInstanceTestServer(t)
	RegisterBuiltinHandlers(srv)
	resp, err := srv.GetHandler("thread.dismiss")(map[string]any{"id": "parent", "cwd": "/work", "config_dir": dir})
	if err != nil {
		t.Fatal(err)
	}
	if resp["status"] != "error" {
		t.Fatalf("dismiss of a parent with open forks should error, got %v", resp["status"])
	}
	// Parent must still be in open/ (not archived).
	if _, found, _ := loadOpenThreadRecord(paths, "parent"); !found {
		t.Error("refused dismiss must leave the parent open")
	}

	// Dismissing the child first, then the parent, both succeed.
	if r, _ := srv.GetHandler("thread.dismiss")(map[string]any{"id": "child", "cwd": "/work", "config_dir": dir}); r["status"] != "ok" {
		t.Fatalf("dismiss child = %v, want ok", r["status"])
	}
	if r, _ := srv.GetHandler("thread.dismiss")(map[string]any{"id": "parent", "cwd": "/work", "config_dir": dir}); r["status"] != "ok" {
		t.Fatalf("dismiss parent after child = %v, want ok", r["status"])
	}
}

func TestOpenForksMessageCount(t *testing.T) {
	dir := t.TempDir()
	paths := config.NewVixPaths(dir, "", "/work")
	srv := newInstanceTestServer(t)
	if msg := srv.openForksMessage(paths, "none"); msg != "" {
		t.Errorf("no forks should yield empty message, got %q", msg)
	}
	for _, id := range []string{"a", "b"} {
		if err := saveThreadRecord(paths, forkRecord(id, "parent", time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	msg := srv.openForksMessage(paths, "parent")
	if msg == "" {
		t.Fatal("expected a refusal message for a parent with 2 open forks")
	}
	if want := "2 open threads were"; !strings.Contains(msg, want) {
		t.Errorf("message = %q, want it to contain %q", msg, want)
	}
}
