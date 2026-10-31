// Copyright 2026 Leadlight Authors
// SPDX-License-Identifier: Apache-2.0

package db

import "testing"

func starTestDB(t *testing.T) *DB {
	t.Helper()
	d := openTestDB(t)
	d.SaveSeries(SeriesRow{ID: 10, Name: "Lorem series"})
	d.SaveSeries(SeriesRow{ID: 20, Name: "Ipsum series"})
	d.SavePatch(PatchRow{
		ID: 1, SeriesID: 10, Name: "Lorem patch", State: "new"})
	d.SavePatch(PatchRow{
		ID: 2, SeriesID: 10, Name: "Dolor patch", State: "new"})
	d.SavePatch(PatchRow{
		ID: 3, SeriesID: 20, Name: "Amet patch", State: "accepted"})
	return d
}

func TestToggleStar(t *testing.T) {
	d := starTestDB(t)
	if d.IsStarred(1) {
		t.Error("patch starred before any toggle")
	}
	if got := d.ToggleStar(1); !got {
		t.Errorf("ToggleStar = %v, want true", got)
	}
	if !d.IsStarred(1) {
		t.Error("patch not starred after toggle")
	}
	if got := d.ToggleStar(1); got {
		t.Errorf("second ToggleStar = %v, want false", got)
	}
	if d.IsStarred(1) {
		t.Error("patch still starred after second toggle")
	}
}

func TestSetAutoStar(t *testing.T) {
	d := starTestDB(t)
	if err := d.SetAutoStar(1, "keyword:lorem"); err != nil {
		t.Fatalf("SetAutoStar: %v", err)
	}
	if !d.IsStarred(1) {
		t.Error("auto star not applied")
	}
}

// An auto rule must never undo a decision the user made by hand,
// or an unwanted star would come straight back on the next sweep.
func TestSetAutoStar_DoesNotOverrideManual(t *testing.T) {
	d := starTestDB(t)
	d.SetAutoStar(1, "keyword:lorem")
	d.ToggleStar(1) // user removes it by hand
	if d.IsStarred(1) {
		t.Fatal("manual unstar did not take effect")
	}
	if err := d.SetAutoStar(1, "keyword:lorem"); err != nil {
		t.Fatalf("SetAutoStar: %v", err)
	}
	if d.IsStarred(1) {
		t.Error("auto star overrode a manual unstar")
	}
}

func TestSetAutoStar_DoesNotDowngradeManualStar(t *testing.T) {
	d := starTestDB(t)
	d.SetStar(1, true)
	d.SetAutoStar(1, "keyword:lorem")
	d.ClearAutoStars()
	if !d.IsStarred(1) {
		t.Error("manual star was cleared with the auto stars")
	}
}

func TestClearAutoStars(t *testing.T) {
	d := starTestDB(t)
	d.SetAutoStar(1, "keyword:lorem")
	d.SetStar(2, true)
	if err := d.ClearAutoStars(); err != nil {
		t.Fatalf("ClearAutoStars: %v", err)
	}
	if d.IsStarred(1) {
		t.Error("auto star survived ClearAutoStars")
	}
	if !d.IsStarred(2) {
		t.Error("manual star removed by ClearAutoStars")
	}
}

func TestGetStarsBatch(t *testing.T) {
	d := starTestDB(t)
	d.SetStar(1, true)
	d.SetStar(2, false)
	d.SetStar(3, true)

	all := d.GetStarsBatch(true, nil)
	if !all[10][1] {
		t.Errorf("patch 1 missing from series 10: %v", all)
	}
	if all[10][2] {
		t.Errorf("unstarred patch 2 present: %v", all)
	}
	if !all[20][3] {
		t.Errorf("patch 3 missing from series 20: %v", all)
	}

	// The state filter selects series, so an accepted patch's
	// series drops out of the active view entirely.
	active := d.GetStarsBatch(false, []string{"new"})
	if !active[10][1] {
		t.Errorf("active series lost its star: %v", active)
	}
	if len(active[20]) != 0 {
		t.Errorf("inactive series 20 present: %v", active)
	}
}

func TestStarEvaluationFlag(t *testing.T) {
	d := starTestDB(t)
	got := d.PatchesNeedingStarEval(10)
	if len(got) != 3 {
		t.Fatalf("PatchesNeedingStarEval = %d patches, want 3", len(got))
	}
	if err := d.MarkStarsEvaluated([]int{1, 2, 3}); err != nil {
		t.Fatalf("MarkStarsEvaluated: %v", err)
	}
	if got := d.PatchesNeedingStarEval(10); len(got) != 0 {
		t.Errorf("still %d patches pending after marking", len(got))
	}
	if err := d.ResetStarEvaluation(); err != nil {
		t.Fatalf("ResetStarEvaluation: %v", err)
	}
	if got := d.PatchesNeedingStarEval(10); len(got) != 3 {
		t.Errorf("reset left %d patches pending, want 3", len(got))
	}
}

// Headers are what the email rule matches on, so they have to
// survive the round trip into StarCandidate.
func TestPatchesNeedingStarEval_CarriesFields(t *testing.T) {
	d := openTestDB(t)
	d.SaveSeries(SeriesRow{ID: 10})
	d.SavePatch(PatchRow{
		ID: 1, SeriesID: 10, Name: "Lorem patch",
		Submitter: "Jane Doe", SubmitterEmail: "jane@example.com",
		State: "new"})
	d.UpdatePatchDetail(1, "content", "diff",
		"Cc: team@example.com\n", "[]")

	got := d.PatchesNeedingStarEval(10)
	if len(got) != 1 {
		t.Fatalf("got %d candidates, want 1", len(got))
	}
	c := got[0]
	if c.SeriesID != 10 || c.Name != "Lorem patch" ||
		c.Submitter != "Jane Doe" ||
		c.SubmitterEmail != "jane@example.com" {
		t.Errorf("candidate = %+v", c)
	}
	if c.Headers != "Cc: team@example.com\n" {
		t.Errorf("Headers = %q", c.Headers)
	}
}

// Detail arriving is the only moment a Cc becomes visible, so it
// has to put the patch back in the queue.
func TestUpdatePatchDetail_RequeuesStarEval(t *testing.T) {
	d := starTestDB(t)
	d.MarkStarsEvaluated([]int{1, 2, 3})
	d.UpdatePatchDetail(1, "content", "diff", "Cc: a@b.c\n", "[]")
	got := d.PatchesNeedingStarEval(10)
	if len(got) != 1 || got[0].ID != 1 {
		t.Errorf("got %+v, want just patch 1 requeued", got)
	}
}
