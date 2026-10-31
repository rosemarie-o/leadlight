// Copyright 2026 Leadlight Authors
// SPDX-License-Identifier: Apache-2.0

package tui

import (
	"fmt"
	"strconv"
	"time"

	"leadlight/status"
)

// refreshSeriesStar recomputes a series row's inherited star from
// its patches and drops the row's cached rendering, which has the
// star glyph baked into it.
func (m *Model) refreshSeriesStar(parentIdx int) {
	rd := &m.RowData[parentIdx]
	rd.Starred = false
	for _, s := range rd.SubRowStarred {
		if s {
			rd.Starred = true
			break
		}
	}
	if len(rd.Data) > 0 {
		if sid, err := strconv.Atoi(rd.Data[ColID]); err == nil {
			m.invalidateSeriesCache([]int{sid})
		}
	}
	m.invalidateVisibleItems()
}

// toggleStar stars or unstars the selected row.  On a series row the
// whole series follows one decision — star everything unless it is
// already fully starred — so the key stays useful without having to
// expand the series first.
func (m *Model) toggleStar() {
	if m.db == nil {
		return
	}
	items := m.getVisibleItems()
	if m.selectedRow >= len(items) {
		return
	}
	item := items[m.selectedRow]
	if item.parentIdx >= len(m.RowData) {
		return
	}
	rd := &m.RowData[item.parentIdx]

	if item.isSubRow {
		patchID, err := strconv.Atoi(item.data[ColID])
		if err != nil {
			return
		}
		starred := m.db.ToggleStar(patchID)
		if item.subRowIdx < len(rd.SubRowStarred) {
			rd.SubRowStarred[item.subRowIdx] = starred
		}
		m.refreshSeriesStar(item.parentIdx)
		m.setStarStatus(starred, 1)
		return
	}

	if len(rd.SubRows) == 0 {
		return
	}
	// Mixed series star fully, so a second press always clears.
	target := false
	for i := range rd.SubRows {
		if i >= len(rd.SubRowStarred) || !rd.SubRowStarred[i] {
			target = true
			break
		}
	}
	changed := 0
	for i, sub := range rd.SubRows {
		patchID, err := strconv.Atoi(sub[ColID])
		if err != nil {
			continue
		}
		if err := m.db.SetStar(patchID, target); err != nil {
			continue
		}
		if i < len(rd.SubRowStarred) {
			rd.SubRowStarred[i] = target
		}
		changed++
	}
	m.refreshSeriesStar(item.parentIdx)
	m.setStarStatus(target, changed)
}

func (m *Model) setStarStatus(starred bool, n int) {
	if m.Status == nil || n == 0 {
		return
	}
	what := "patch"
	if n > 1 {
		what = fmt.Sprintf("%d patches", n)
	}
	verb := "Unstarred"
	if starred {
		verb = "Starred"
	}
	m.Status.SetTimed(status.Info,
		fmt.Sprintf("%s %s", verb, what), 3*time.Second)
}
