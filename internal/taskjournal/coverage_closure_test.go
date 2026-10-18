package taskjournal

import (
	"path/filepath"
	"testing"
	"time"
)

func TestCoverageClosureValidatesScopesFiltersAndWrappers(t *testing.T) {
	if err := (*Journal)(nil).Close(); err != nil {
		t.Fatalf("nil close: %v", err)
	}
	if _, _, err := (*Journal)(nil).Replay(0, 1); err == nil {
		t.Fatal("nil replay accepted")
	}
	if status := (*Journal)(nil).Status(); status.Storage != StorageDegraded || status.Detail != "journal unavailable" {
		t.Fatalf("nil status=%+v", status)
	}

	for _, filter := range []TaskFilter{
		{Controller: "owner"},
		{State: "unknown"},
		{Operation: "../bad"},
		{ProjectID: "edge_0123456789abcdef01234567"},
		{EdgeID: "prj_0123456789abcdef01234567"},
	} {
		if err := filter.validate(); err == nil {
			t.Fatalf("invalid filter accepted: %+v", filter)
		}
	}

	now := time.Date(2026, 7, 17, 18, 0, 0, 0, time.UTC)
	base, err := newEntry(taskID(980), "repo_status", "http", StateExecuting, now)
	if err != nil {
		t.Fatal(err)
	}
	invalidEntries := []Entry{
		func() Entry { value := base; value.ProjectID = "edge_0123456789abcdef01234567"; return value }(),
		func() Entry { value := base; value.EdgeID = "prj_0123456789abcdef01234567"; return value }(),
		func() Entry { value := base; value.Summary = "private"; return value }(),
		func() Entry { value := base; value.Controller = "owner"; return value }(),
		func() Entry { value := base; value.State = "unknown"; return value }(),
		func() Entry { value := base; value.CreatedAt = time.Time{}; return value }(),
	}
	for _, entry := range invalidEntries {
		if err := entry.validate(); err == nil {
			t.Fatalf("invalid entry accepted: %+v", entry)
		}
	}

	store, err := OpenSQLiteStore(filepath.Join(t.TempDir(), "tasks"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	base.ProjectID = "prj_0123456789abcdef01234567"
	base.EdgeID = "edge_0123456789abcdef01234567"
	if _, _, err := store.Create(base); err != nil {
		t.Fatal(err)
	}
	page, err := store.ListPage(1, "")
	if err != nil || len(page.Entries) != 1 || page.Entries[0].ProjectID != base.ProjectID || page.Entries[0].EdgeID != base.EdgeID {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	if _, err := store.ListPageFiltered(1, "", TaskFilter{ProjectID: base.ProjectID, EdgeID: base.EdgeID}); err != nil {
		t.Fatal(err)
	}
}
