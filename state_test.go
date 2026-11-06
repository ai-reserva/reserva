package main

import (
	"path/filepath"
	"testing"
)

func TestArchiveState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reserva", "archived.json")
	a := loadArchive(path)
	if !a.toggle("a") || !a.toggle("b") || a.toggle("b") {
		t.Fatal("toggle should archive, then unarchive")
	}
	if err := a.save(); err != nil {
		t.Fatal(err)
	}

	s1, s2 := &Session{ID: "a"}, &Session{ID: "b", Archived: true}
	loadArchive(path).apply([]*Group{{Sessions: []*Session{s1, s2}}})
	if !s1.Archived || s2.Archived {
		t.Errorf("after reload: a archived = %v, b archived = %v, want true, false", s1.Archived, s2.Archived)
	}
}

func TestLoadArchiveMissing(t *testing.T) {
	if a := loadArchive(filepath.Join(t.TempDir(), "nope.json")); len(a.ids) != 0 {
		t.Errorf("missing file gave %v, want an empty archive", a.ids)
	}
}
