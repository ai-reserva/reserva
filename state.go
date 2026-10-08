package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// archiveState is the set of archived session IDs. It is reserva's own
// state and lives under $XDG_STATE_HOME, never inside Claude's directories.
type archiveState struct {
	path string
	ids  map[string]bool
}

func defaultStatePath() string {
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return filepath.Join(d, "reserva", "archived.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "reserva", "archived.json")
}

func loadArchive(path string) *archiveState {
	st := &archiveState{path: path, ids: map[string]bool{}}
	data, err := os.ReadFile(path)
	if err != nil {
		return st
	}
	var ids []string
	if json.Unmarshal(data, &ids) == nil {
		for _, id := range ids {
			st.ids[id] = true
		}
	}
	return st
}

func (a *archiveState) save() error {
	ids := make([]string, 0, len(a.ids))
	for id := range a.ids {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	data, err := json.MarshalIndent(ids, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.path), 0o755); err != nil {
		return err
	}
	tmp := a.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, a.path)
}

func (a *archiveState) toggle(id string) bool {
	if a.ids[id] {
		delete(a.ids, id)
		return false
	}
	a.ids[id] = true
	return true
}

func (a *archiveState) apply(groups []*Group) {
	for _, g := range groups {
		for _, s := range g.Sessions {
			s.Archived = a.ids[s.ID]
		}
	}
}
