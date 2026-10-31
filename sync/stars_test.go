// Copyright 2026 Leadlight Authors
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"net/http"
	"testing"

	"leadlight/config"
	"leadlight/db"
)

func TestStarHeaderValue(t *testing.T) {
	tests := []struct {
		name    string
		headers string
		key     string
		want    string
	}{
		{
			name:    "simple",
			headers: "From: a@b.c\nCc: team@example.com\n",
			key:     "Cc",
			want:    "team@example.com",
		},
		{
			// Recipient lists are routinely folded, and a Cc
			// is unmatchable unless the lines are rejoined.
			name: "folded continuation",
			headers: "Cc: one@example.com,\n" +
				"\ttwo@example.com,\n" +
				" three@example.com\nSubject: x\n",
			key:  "Cc",
			want: "one@example.com, two@example.com, three@example.com",
		},
		{
			name:    "repeated header",
			headers: "Cc: a@example.com\nCc: b@example.com\n",
			key:     "Cc",
			want:    "a@example.com b@example.com",
		},
		{
			name:    "missing",
			headers: "From: a@b.c\n",
			key:     "Cc",
			want:    "",
		},
		{
			name:    "does not match a prefix of another header",
			headers: "Cc-Original: nope@example.com\n",
			key:     "Cc",
			want:    "",
		},
		{
			name:    "legacy json string",
			headers: `{"Cc": "team@example.com"}`,
			key:     "Cc",
			want:    "team@example.com",
		},
		{
			name:    "legacy json list",
			headers: `{"Cc": ["a@example.com", "b@example.com"]}`,
			key:     "Cc",
			want:    "a@example.com b@example.com",
		},
		{
			name:    "malformed json",
			headers: `{not json`,
			key:     "Cc",
			want:    "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := starHeaderValue(tt.headers, tt.key)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMatchStarRules(t *testing.T) {
	cfg := &config.Config{
		StarEmails:     []string{"me@example.com"},
		StarKeywords:   []string{"netdev"},
		StarSubmitters: []string{"Jane Doe"},
	}
	tests := []struct {
		name      string
		candidate db.StarCandidate
		wantRule  string
		wantOK    bool
	}{
		{
			name:      "no match",
			candidate: db.StarCandidate{Name: "[PATCH] unrelated change"},
		},
		{
			name:      "keyword in subject",
			candidate: db.StarCandidate{Name: "[PATCH net] netdev: fix"},
			wantRule:  "keyword:netdev",
			wantOK:    true,
		},
		{
			name:      "keyword is case insensitive",
			candidate: db.StarCandidate{Name: "[PATCH] NETDEV rework"},
			wantRule:  "keyword:netdev",
			wantOK:    true,
		},
		{
			name: "submitter by name",
			candidate: db.StarCandidate{
				Name: "[PATCH] something", Submitter: "Jane Doe"},
			wantRule: "submitter:Jane Doe",
			wantOK:   true,
		},
		{
			name: "cc matches",
			candidate: db.StarCandidate{
				Name:    "[PATCH] something",
				Headers: "Cc: me@example.com, you@example.com\n"},
			wantRule: "email:me@example.com",
			wantOK:   true,
		},
		{
			name: "to matches",
			candidate: db.StarCandidate{
				Name:    "[PATCH] something",
				Headers: "To: Me <me@example.com>\n"},
			wantRule: "email:me@example.com",
			wantOK:   true,
		},
		{
			// The address only appears once detail is fetched,
			// so an empty header block must simply not match.
			name: "no headers yet",
			candidate: db.StarCandidate{
				Name: "[PATCH] something", Headers: ""},
		},
		{
			name: "from alone does not count",
			candidate: db.StarCandidate{
				Name:    "[PATCH] something",
				Headers: "From: me@example.com\n"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule, ok := matchStarRules(tt.candidate, cfg)
			if ok != tt.wantOK || rule != tt.wantRule {
				t.Errorf("got (%q, %v), want (%q, %v)",
					rule, ok, tt.wantRule, tt.wantOK)
			}
		})
	}
}

func TestMatchStarRules_Disabled(t *testing.T) {
	no := false
	cfg := &config.Config{
		StarKeywords: []string{"netdev"},
		AutoStar:     &no,
	}
	c := db.StarCandidate{Name: "[PATCH] netdev: fix"}
	if _, ok := matchStarRules(c, cfg); ok {
		t.Error("rule matched while auto-starring is disabled")
	}
}

func emptyHandler() http.Handler {
	return http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(testProjectJSON))
		})
}

func TestStarSweepStep(t *testing.T) {
	s, d := setupSyncer(t, emptyHandler())
	s.cfg.StarKeywords = []string{"netdev"}

	d.SaveSeries(db.SeriesRow{ID: 10})
	d.SavePatch(db.PatchRow{
		ID: 1, SeriesID: 10, Name: "[PATCH] netdev: fix", State: "new"})
	d.SavePatch(db.PatchRow{
		ID: 2, SeriesID: 10, Name: "[PATCH] unrelated", State: "new"})

	if got := s.starSweepStep(context.Background()); got != 10 {
		t.Errorf("starSweepStep = %d, want series 10", got)
	}
	if !d.IsStarred(1) {
		t.Error("matching patch not starred")
	}
	if d.IsStarred(2) {
		t.Error("non-matching patch starred")
	}
	// Everything is evaluated, so a second pass has no work and
	// must not keep waking the loop up.
	if got := s.starSweepStep(context.Background()); got != 0 {
		t.Errorf("second starSweepStep = %d, want 0", got)
	}
}

func TestStarSweepStep_NoRules(t *testing.T) {
	s, d := setupSyncer(t, emptyHandler())
	d.SaveSeries(db.SeriesRow{ID: 10})
	d.SavePatch(db.PatchRow{
		ID: 1, SeriesID: 10, Name: "[PATCH] netdev: fix", State: "new"})

	if got := s.starSweepStep(context.Background()); got != 0 {
		t.Errorf("starSweepStep = %d, want 0 with no rules", got)
	}
	if d.IsStarred(1) {
		t.Error("patch starred with no rules configured")
	}
}

func TestMigrateStarRules(t *testing.T) {
	s, d := setupSyncer(t, emptyHandler())
	s.cfg.StarKeywords = []string{"netdev"}

	d.SaveSeries(db.SeriesRow{ID: 10})
	d.SavePatch(db.PatchRow{
		ID: 1, SeriesID: 10, Name: "[PATCH] netdev: fix", State: "new"})
	d.SavePatch(db.PatchRow{
		ID: 2, SeriesID: 10, Name: "[PATCH] vhost: fix", State: "new"})
	d.SetStar(2, true) // starred by hand

	MigrateStarRules(d, s.cfg)
	s.starSweepStep(context.Background())
	if !d.IsStarred(1) {
		t.Fatal("rule did not star the matching patch")
	}

	// Rules change: the derived star goes, the manual one stays.
	s.cfg.StarKeywords = []string{"vhost"}
	MigrateStarRules(d, s.cfg)
	if d.IsStarred(1) {
		t.Error("stale auto star survived a rule change")
	}
	if !d.IsStarred(2) {
		t.Error("manual star lost on a rule change")
	}
	s.starSweepStep(context.Background())
	if !d.IsStarred(2) {
		t.Error("patch matching the new rule is not starred")
	}
}

func TestMigrateStarRules_UnchangedIsNoop(t *testing.T) {
	s, d := setupSyncer(t, emptyHandler())
	s.cfg.StarKeywords = []string{"netdev"}

	d.SaveSeries(db.SeriesRow{ID: 10})
	d.SavePatch(db.PatchRow{
		ID: 1, SeriesID: 10, Name: "[PATCH] netdev: fix", State: "new"})

	MigrateStarRules(d, s.cfg)
	s.starSweepStep(context.Background())
	MigrateStarRules(d, s.cfg)

	// A no-op migration must not queue the whole table for
	// re-evaluation on every single startup.
	if got := d.PatchesNeedingStarEval(10); len(got) != 0 {
		t.Errorf("%d patches requeued by an unchanged rule set", len(got))
	}
	if !d.IsStarred(1) {
		t.Error("star lost across an unchanged migration")
	}
}
