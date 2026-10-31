// Copyright 2026 Leadlight Authors
// SPDX-License-Identifier: Apache-2.0

package db

import (
	"database/sql/driver"
	"strings"
	"time"
)

// Star sources.  A manual star is a decision the user made by hand
// and always wins over a rule; an auto star is derived from the
// configured rules and is thrown away whenever those rules change.
const (
	StarManual = "manual"
	StarAuto   = "auto"
)

// IsStarred reports whether a patch is currently starred.
func (d *DB) IsStarred(patchID int) bool {
	var starred int
	err := d.conn.QueryRow(
		`SELECT starred FROM patch_stars WHERE patch_id = ?`,
		patchID).Scan(&starred)
	return err == nil && starred == 1
}

// SetStar records an explicit, user-made decision about a patch.
// The row is always written, even when unstarring: its absence and
// a starred = 0 row mean different things to the rule engine, which
// treats the latter as "the user said no, leave it alone".
func (d *DB) SetStar(patchID int, starred bool) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	v := 0
	if starred {
		v = 1
	}
	_, err := d.conn.Exec(`
		INSERT INTO patch_stars
			(patch_id, starred, source, rule, created_at)
		VALUES (?, ?, ?, '', ?)
		ON CONFLICT(patch_id) DO UPDATE SET
			starred = excluded.starred,
			source = excluded.source,
			rule = '',
			created_at = excluded.created_at`,
		patchID, v, StarManual, time.Now().UTC().Format(time.RFC3339))
	return err
}

// ToggleStar flips a patch's star and reports the new state.
func (d *DB) ToggleStar(patchID int) bool {
	starred := !d.IsStarred(patchID)
	d.SetStar(patchID, starred)
	return starred
}

// SetAutoStar stars a patch because a rule matched.  A patch the
// user has already ruled on by hand is left untouched, so clearing
// an unwanted auto star makes it stay cleared.
func (d *DB) SetAutoStar(patchID int, rule string) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	_, err := d.conn.Exec(`
		INSERT INTO patch_stars
			(patch_id, starred, source, rule, created_at)
		VALUES (?, 1, ?, ?, ?)
		ON CONFLICT(patch_id) DO UPDATE SET
			starred = 1,
			rule = excluded.rule,
			created_at = excluded.created_at
		WHERE patch_stars.source = ?`,
		patchID, StarAuto, rule,
		time.Now().UTC().Format(time.RFC3339), StarAuto)
	return err
}

// ClearAutoStars removes every rule-derived star, leaving manual
// ones alone.  Used when the configured rules change and the whole
// derived set has to be rebuilt.
func (d *DB) ClearAutoStars() error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	_, err := d.conn.Exec(
		`DELETE FROM patch_stars WHERE source = ?`, StarAuto)
	return err
}

// GetStarsBatch returns the starred patches of all series matching
// the current view filter, grouped by series ID.  Mirrors the other
// batch getters so the TUI can load stars in one query per refresh.
func (d *DB) GetStarsBatch(
	showAll bool, states []string,
) map[int]map[int]bool {
	sub, args := d.seriesIDSubquery(showAll, states)
	query := `SELECT p.series_id, s.patch_id
		FROM patch_stars s
		JOIN patches p ON s.patch_id = p.id
		WHERE s.starred = 1
		  AND p.series_id IN (` + sub + `)`
	result := map[int]map[int]bool{}
	d.queryRows(query, args, 2, func(dest []driver.Value) {
		seriesID := valInt(dest[0])
		if result[seriesID] == nil {
			result[seriesID] = map[int]bool{}
		}
		result[seriesID][valInt(dest[1])] = true
	})
	return result
}

// StarCandidate is the subset of a patch the star rules look at.
type StarCandidate struct {
	ID             int
	SeriesID       int
	Name           string
	Submitter      string
	SubmitterEmail string
	Headers        string
}

// PatchesNeedingStarEval returns patches whose star rules have not
// been evaluated since they last changed, newest first.
func (d *DB) PatchesNeedingStarEval(limit int) []StarCandidate {
	query := `SELECT id, series_id, COALESCE(name, ''),
			COALESCE(submitter, ''), COALESCE(submitter_email, ''),
			COALESCE(headers, '')
		FROM patches
		WHERE stars_evaluated = 0
		ORDER BY id DESC
		LIMIT ?`
	var out []StarCandidate
	d.queryRows(query, []interface{}{limit}, 6,
		func(dest []driver.Value) {
			sc := scanCtx{"patches", valInt(dest[0])}
			out = append(out, StarCandidate{
				ID:             sc.id,
				SeriesID:       sc.int_(dest[1], "series_id"),
				Name:           sc.strNN(dest[2], "name"),
				Submitter:      sc.strNN(dest[3], "submitter"),
				SubmitterEmail: sc.strNN(dest[4], "submitter_email"),
				Headers:        sc.strNN(dest[5], "headers"),
			})
		})
	return out
}

// MarkStarsEvaluated records that the rules have been applied to
// these patches, so the sweep does not look at them again.
func (d *DB) MarkStarsEvaluated(patchIDs []int) error {
	if len(patchIDs) == 0 {
		return nil
	}
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	ph := make([]string, len(patchIDs))
	args := make([]interface{}, len(patchIDs))
	for i, id := range patchIDs {
		ph[i] = "?"
		args[i] = id
	}
	_, err := d.conn.Exec(
		`UPDATE patches SET stars_evaluated = 1 WHERE id IN (`+
			strings.Join(ph, ",")+`)`, args...)
	return err
}

// ResetStarEvaluation queues every patch for re-evaluation.
func (d *DB) ResetStarEvaluation() error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	_, err := d.conn.Exec(
		`UPDATE patches SET stars_evaluated = 0
		 WHERE stars_evaluated != 0`)
	return err
}
