package labelgroupimport

import (
	"reflect"
	"testing"

	"github.com/brian1917/illumioapi"
)

func newEntry(line int, name string, pending ...string) entry {
	return entry{csvLine: line, labelGroup: illumioapi.LabelGroup{Key: "app", Name: name}, pendingSubGroups: pending}
}

func names(entries []entry) []string {
	out := []string{}
	for _, e := range entries {
		out = append(out, e.labelGroup.Name)
	}
	return out
}

func droppedNames(dropped []droppedEntry) []string {
	out := []string{}
	for _, d := range dropped {
		out = append(out, d.entry.labelGroup.Name)
	}
	return out
}

func TestOrderLabelGroups(t *testing.T) {
	tests := []struct {
		name        string
		newLGs      []entry
		updates     []entry
		wantOrder   []string
		wantUpdates []string
		wantDropped []string
	}{
		{
			name:        "no dependencies keeps csv order",
			newLGs:      []entry{newEntry(2, "A"), newEntry(3, "B")},
			wantOrder:   []string{"A", "B"},
			wantUpdates: []string{},
			wantDropped: []string{},
		},
		{
			name:        "parent listed before child is reordered",
			newLGs:      []entry{newEntry(2, "Parent", "appChild"), newEntry(3, "Child")},
			wantOrder:   []string{"Child", "Parent"},
			wantUpdates: []string{},
			wantDropped: []string{},
		},
		{
			name:        "chain of three",
			newLGs:      []entry{newEntry(2, "A", "appB"), newEntry(3, "B", "appC"), newEntry(4, "C")},
			wantOrder:   []string{"C", "B", "A"},
			wantUpdates: []string{},
			wantDropped: []string{},
		},
		{
			name:        "shared child with multiple parents",
			newLGs:      []entry{newEntry(2, "P1", "appC"), newEntry(3, "P2", "appC"), newEntry(4, "C")},
			wantOrder:   []string{"C", "P1", "P2"},
			wantUpdates: []string{},
			wantDropped: []string{},
		},
		{
			name:        "cycle is dropped and dependents cascade",
			newLGs:      []entry{newEntry(2, "A", "appB"), newEntry(3, "B", "appA"), newEntry(4, "C", "appA"), newEntry(5, "D")},
			wantOrder:   []string{"D"},
			wantUpdates: []string{},
			wantDropped: []string{"B", "A", "C"},
		},
		{
			name:        "self reference is dropped",
			newLGs:      []entry{newEntry(2, "A", "appA")},
			wantOrder:   []string{},
			wantUpdates: []string{},
			wantDropped: []string{"A"},
		},
		{
			name:        "missing dependency is dropped",
			newLGs:      []entry{newEntry(2, "A", "appMissing"), newEntry(3, "B", "appA")},
			wantOrder:   []string{},
			wantUpdates: []string{},
			wantDropped: []string{"A", "B"},
		},
		{
			name:        "updates depending on created and dropped groups",
			newLGs:      []entry{newEntry(2, "Good"), newEntry(3, "Bad", "appBad")},
			updates:     []entry{newEntry(4, "U1", "appGood"), newEntry(5, "U2", "appBad"), newEntry(6, "U3")},
			wantOrder:   []string{"Good"},
			wantUpdates: []string{"U1", "U3"},
			wantDropped: []string{"Bad", "U2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ordered, updates, dropped := orderLabelGroups(tt.newLGs, tt.updates)
			if got := names(ordered); !reflect.DeepEqual(got, tt.wantOrder) {
				t.Errorf("order = %v, want %v", got, tt.wantOrder)
			}
			if got := names(updates); !reflect.DeepEqual(got, tt.wantUpdates) {
				t.Errorf("updates = %v, want %v", got, tt.wantUpdates)
			}
			if got := droppedNames(dropped); !reflect.DeepEqual(got, tt.wantDropped) {
				t.Errorf("dropped = %v, want %v", got, tt.wantDropped)
			}
		})
	}
}

func TestResolvePending(t *testing.T) {
	created := map[string]string{"appB": "/orgs/1/sec_policy/draft/label_groups/b"}

	e := newEntry(2, "A", "appB")
	e.labelGroup.SubGroups = []*illumioapi.SubGroups{{Href: "/orgs/1/sec_policy/draft/label_groups/existing"}}
	if missing := resolvePending(&e, created); missing != "" {
		t.Fatalf("unexpected missing %s", missing)
	}
	if len(e.labelGroup.SubGroups) != 2 || e.labelGroup.SubGroups[1].Href != created["appB"] {
		t.Errorf("subgroups not resolved: %+v", e.labelGroup.SubGroups)
	}

	e2 := newEntry(3, "X", "appNotCreated")
	if missing := resolvePending(&e2, created); missing != "appNotCreated" {
		t.Errorf("missing = %q, want appNotCreated", missing)
	}
}
