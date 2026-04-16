package version

import "testing"

func TestGetReturnsBuildInfo(t *testing.T) {
	oldVersion := Version
	oldCommit := Commit
	oldDate := Date
	t.Cleanup(func() {
		Version = oldVersion
		Commit = oldCommit
		Date = oldDate
	})

	Version = "v1.2.3"
	Commit = "abc123"
	Date = "2026-04-15T10:00:00Z"

	info := Get()
	if info.Version != Version || info.Commit != Commit || info.Date != Date {
		t.Fatalf("unexpected build info: %+v", info)
	}
}
