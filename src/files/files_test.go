package files

import "testing"

// The fileset must be refreshed from the master index at every
// scan: new files get the current repo version, known files keep
// their first-published version (a mirror only needs to be as fresh
// as the files it is asked to serve), and vanished files are pruned.
func TestUpdateFromIndexRefreshesFileset(t *testing.T) {
	f := Fileset{Files: map[string]File{
		"/pool/old":     {Uri: "/pool/old", Version: 5},
		"/pool/removed": {Uri: "/pool/removed", Version: 5},
	}}

	if err := f.updateFromIndex([]byte("!version 42\n/pool/old\n/pool/new\n")); err != nil {
		t.Fatalf("updateFromIndex: %v", err)
	}

	if len(f.Files) != 2 {
		t.Fatalf("expected 2 files, got %d: %v", len(f.Files), f.Files)
	}
	if v := f.Files["/pool/old"].Version; v != 5 {
		t.Errorf("known file must keep its first-published version, got %d", v)
	}
	if v := f.Files["/pool/new"].Version; v != 42 {
		t.Errorf("new file must be stamped with the current repo version, got %d", v)
	}
	if _, ok := f.Files["/pool/removed"]; ok {
		t.Errorf("file no longer published must be pruned")
	}
}

// An invalid index must leave the current fileset untouched.
func TestUpdateFromIndexInvalid(t *testing.T) {
	f := Fileset{Files: map[string]File{
		"/pool/keep": {Uri: "/pool/keep", Version: 1},
	}}
	if err := f.updateFromIndex([]byte("garbage without version string")); err == nil {
		t.Fatal("expected error for index without version string")
	}
	if len(f.Files) != 1 {
		t.Fatal("fileset must be left untouched on invalid index")
	}
}
