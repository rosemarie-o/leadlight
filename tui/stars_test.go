// Copyright 2026 Leadlight Authors
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"strings"
	"testing"

	"leadlight/db"
	"leadlight/status"
)

// starModel builds a model backed by a real in-memory database,
// which toggleStar needs: testModel() has no db at all.
func starModel(t *testing.T) (*Model, *db.DB) {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })

	d.SaveSeries(db.SeriesRow{ID: 1, Name: "Lorem series"})
	d.SavePatch(db.PatchRow{
		ID: 11, SeriesID: 1, Name: "[PATCH 1/2] Lorem one", State: "new"})
	d.SavePatch(db.PatchRow{
		ID: 12, SeriesID: 1, Name: "[PATCH 2/2] Lorem two", State: "new"})
	d.SaveSeries(db.SeriesRow{ID: 2, Name: "Ipsum series"})
	d.SavePatch(db.PatchRow{
		ID: 21, SeriesID: 2, Name: "[PATCH] Ipsum one", State: "new"})

	m := NewModel(d, []string{"new", "under-review"}, "")
	m.Status = status.NewRegistry(nil)
	m.width = 120
	m.height = 30
	return m, d
}

// rowFor returns the index of the visible row with the given ID.
func rowFor(t *testing.T, m *Model, id string) int {
	t.Helper()
	for i, item := range m.getVisibleItems() {
		if len(item.data) > 0 && item.data[ColID] == id {
			return i
		}
	}
	t.Fatalf("no visible row with ID %q", id)
	return -1
}

func TestToggleStar_Patch(t *testing.T) {
	m, d := starModel(t)
	m.RowData[0].Expanded = true
	m.invalidateVisibleItems()

	m.selectedRow = rowFor(t, m, "11")
	m.toggleStar()

	if !d.IsStarred(11) {
		t.Error("patch not starred in the database")
	}
	if !m.RowData[0].SubRowStarred[0] {
		t.Error("sub-row not marked starred")
	}
	// A series inherits its patches' stars so the mark survives
	// collapsing the series.
	if !m.RowData[0].Starred {
		t.Error("series did not inherit the star")
	}
	if d.IsStarred(12) {
		t.Error("starring one patch starred its sibling")
	}

	m.selectedRow = rowFor(t, m, "11")
	m.toggleStar()
	if d.IsStarred(11) {
		t.Error("second toggle did not unstar")
	}
	if m.RowData[0].Starred {
		t.Error("series still starred after its only star went")
	}
}

func TestToggleStar_SeriesStarsAllPatches(t *testing.T) {
	m, d := starModel(t)
	m.selectedRow = rowFor(t, m, "1")
	m.toggleStar()

	if !d.IsStarred(11) || !d.IsStarred(12) {
		t.Error("starring a series did not star all of its patches")
	}
	if !m.RowData[0].Starred {
		t.Error("series not marked starred")
	}

	m.selectedRow = rowFor(t, m, "1")
	m.toggleStar()
	if d.IsStarred(11) || d.IsStarred(12) {
		t.Error("second toggle did not clear the series")
	}
}

// A partly starred series should fill up rather than clear, so the
// key always has a predictable first effect.
func TestToggleStar_PartialSeriesStarsRest(t *testing.T) {
	m, d := starModel(t)
	m.RowData[0].Expanded = true
	m.invalidateVisibleItems()
	m.selectedRow = rowFor(t, m, "11")
	m.toggleStar()

	m.selectedRow = rowFor(t, m, "1")
	m.toggleStar()
	if !d.IsStarred(11) || !d.IsStarred(12) {
		t.Error("a partly starred series did not fill up")
	}
}

func TestToggleStar_SurvivesReload(t *testing.T) {
	m, _ := starModel(t)
	m.selectedRow = rowFor(t, m, "1")
	m.toggleStar()

	m.reloadData()
	if !m.RowData[0].Starred {
		t.Error("star lost on reload")
	}
	if !m.RowData[0].SubRowStarred[0] {
		t.Error("sub-row star lost on reload")
	}
}

func TestStarredOnlyFilter(t *testing.T) {
	m, _ := starModel(t)
	m.RowData[0].Expanded = true
	m.invalidateVisibleItems()
	m.selectedRow = rowFor(t, m, "11")
	m.toggleStar()

	m.starredOnly = true
	m.invalidateVisibleItems()

	var ids []string
	for _, item := range m.getVisibleItems() {
		ids = append(ids, item.data[ColID])
	}
	// The starred patch and its series, and nothing else: the
	// unstarred sibling and the other series both drop out.
	want := []string{"1", "11"}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Errorf("visible IDs = %v, want %v", ids, want)
	}
}

// The filter has to expand on its own, or a starred patch would be
// invisible inside a collapsed series.
func TestStarredOnlyFilter_AutoExpands(t *testing.T) {
	m, _ := starModel(t)
	m.selectedRow = rowFor(t, m, "1")
	m.toggleStar()
	// Collapse, then filter.
	m.RowData[0].Expanded = false
	m.starredOnly = true
	m.invalidateVisibleItems()

	found := false
	for _, item := range m.getVisibleItems() {
		if item.isSubRow && item.data[ColID] == "11" {
			found = true
		}
	}
	if !found {
		t.Error("starred patch hidden inside a collapsed series")
	}
}

func TestStarredOnlyFilter_EmptyWhenNothingStarred(t *testing.T) {
	m, _ := starModel(t)
	m.starredOnly = true
	m.invalidateVisibleItems()
	if got := m.getVisibleItems(); len(got) != 0 {
		t.Errorf("%d rows visible with nothing starred", len(got))
	}
}

func TestStarKeys(t *testing.T) {
	m, d := starModel(t)
	m.selectedRow = rowFor(t, m, "1")

	m = pressKey(m, "*")
	if !d.IsStarred(11) {
		t.Error("* did not star the selected series")
	}

	m = pressKey(m, "!")
	if !m.starredOnly {
		t.Error("! did not turn the starred filter on")
	}
	m = pressKey(m, "!")
	if m.starredOnly {
		t.Error("! did not turn the starred filter off")
	}
}

// The rendered row is cached per series with the gutter baked in,
// so a toggle that skipped invalidation would leave a stale glyph.
func TestToggleStar_InvalidatesRenderCache(t *testing.T) {
	m, _ := starModel(t)
	before := m.View()
	if strings.Contains(before, starGlyph) {
		t.Fatal("star glyph present before anything was starred")
	}
	m.selectedRow = rowFor(t, m, "1")
	m.toggleStar()
	if after := m.View(); !strings.Contains(after, starGlyph) {
		t.Error("star glyph missing from the view after starring")
	}
}

func TestRowGutter(t *testing.T) {
	m := testModel()
	tests := []struct {
		name     string
		item     visibleItem
		selected bool
		fetching bool
		want     string
	}{
		{"plain", visibleItem{fetched: true}, false, false, "   "},
		{"selected", visibleItem{fetched: true}, true, false, "▸  "},
		{"unfetched", visibleItem{}, false, false, " · "},
		{"starred", visibleItem{fetched: true, starred: true},
			false, false, "  ★"},
		{"selected unfetched starred",
			visibleItem{starred: true}, true, false, "▸·★"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := m.rowGutter(tt.item, tt.selected, tt.fetching)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
			if len([]rune(got)) != indicatorWidth {
				t.Errorf("got %d cells, want %d",
					len([]rune(got)), indicatorWidth)
			}
		})
	}
}
