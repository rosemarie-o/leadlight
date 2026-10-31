// Copyright 2026 Leadlight Authors
// SPDX-License-Identifier: Apache-2.0

package sync

import (
	"context"
	"encoding/json"
	"log"
	"strings"

	"leadlight/config"
	"leadlight/db"
)

// Patches evaluated per sweep step.  Matching is pure string work on
// rows already in the database, so this can be far larger than the
// API-bound background loops without costing anything noticeable.
const starSweepBatch = 200

// starHeaderValue returns a header's value from a stored header
// blob, joining RFC-822 continuation lines.  Recipient lists are
// routinely folded across lines, so a Cc can only be matched
// reliably once it has been rejoined.
//
// This duplicates a few lines of tui/mbox.go on purpose: sync must
// not import tui, and hoisting the originals into a shared package
// would churn the mbox renderer for no behavioural gain.
func starHeaderValue(headers, name string) string {
	if strings.HasPrefix(headers, "{") {
		// Legacy JSON blobs, written before headers were stored
		// in the compact form.
		var raw map[string]interface{}
		if err := json.Unmarshal([]byte(headers), &raw); err != nil {
			return ""
		}
		switch v := raw[name].(type) {
		case string:
			return v
		case []interface{}:
			var parts []string
			for _, item := range v {
				if s, ok := item.(string); ok {
					parts = append(parts, s)
				}
			}
			return strings.Join(parts, " ")
		}
		return ""
	}

	prefix := name + ":"
	var result []string
	found := false
	for _, line := range strings.Split(headers, "\n") {
		if strings.HasPrefix(line, prefix) {
			found = true
			result = append(result,
				strings.TrimSpace(line[len(prefix):]))
		} else if found && len(line) > 0 &&
			(line[0] == ' ' || line[0] == '\t') {
			result = append(result, strings.TrimSpace(line))
		} else {
			found = false
		}
	}
	return strings.Join(result, " ")
}

func containsFold(haystack string, needles []string) (string, bool) {
	if haystack == "" {
		return "", false
	}
	lower := strings.ToLower(haystack)
	for _, n := range needles {
		if n == "" {
			continue
		}
		if strings.Contains(lower, strings.ToLower(n)) {
			return n, true
		}
	}
	return "", false
}

// matchStarRules reports whether a patch matches any configured
// auto-star rule, and returns a short description of the rule that
// matched for storing alongside the star.
func matchStarRules(
	p db.StarCandidate, cfg *config.Config,
) (string, bool) {
	if cfg == nil || !cfg.StarRules() {
		return "", false
	}
	if m, ok := containsFold(p.Name, cfg.StarKeywords); ok {
		return "keyword:" + m, true
	}
	if m, ok := containsFold(p.Submitter, cfg.StarSubmitters); ok {
		return "submitter:" + m, true
	}
	if m, ok := containsFold(p.SubmitterEmail, cfg.StarSubmitters); ok {
		return "submitter:" + m, true
	}
	if len(cfg.StarEmails) > 0 {
		recipients := starHeaderValue(p.Headers, "To") + " " +
			starHeaderValue(p.Headers, "Cc")
		if m, ok := containsFold(recipients, cfg.StarEmails); ok {
			return "email:" + m, true
		}
	}
	return "", false
}

// starSweepStep evaluates the auto-star rules for a batch of patches
// that have not been judged yet, and returns the series ID of one
// patch it starred so the caller can refresh that row (0 when
// nothing changed).  Shaped to fit runBgLoop.
func (s *Syncer) starSweepStep(ctx context.Context) int {
	if !s.cfg.StarRules() {
		return 0
	}
	candidates := s.db.PatchesNeedingStarEval(starSweepBatch)
	if len(candidates) == 0 {
		return 0
	}
	ids := make([]int, 0, len(candidates))
	starred := 0
	seriesID := 0
	for _, c := range candidates {
		ids = append(ids, c.ID)
		rule, ok := matchStarRules(c, s.cfg)
		if !ok {
			continue
		}
		if err := s.db.SetAutoStar(c.ID, rule); err != nil {
			log.Printf("SYNC: star patch %d: %v", c.ID, err)
			continue
		}
		starred++
		if seriesID == 0 {
			seriesID = c.SeriesID
		}
	}
	s.db.MarkStarsEvaluated(ids)
	if starred > 0 {
		log.Printf("SYNC: auto-starred %d of %d patches",
			starred, len(candidates))
	}
	return seriesID
}

// MigrateStarRules rebuilds the rule-derived stars when the
// configured rules have changed since the last run.  Stars the user
// set by hand are left alone; only the derived set is recomputed,
// which the background sweep then does incrementally.
func MigrateStarRules(d *db.DB, cfg *config.Config) {
	hash := cfg.StarRulesHash()
	if d.GetSyncState("star_rules") == hash {
		return
	}
	if err := d.ClearAutoStars(); err != nil {
		log.Printf("SYNC: clear auto stars: %v", err)
		return
	}
	if err := d.ResetStarEvaluation(); err != nil {
		log.Printf("SYNC: reset star evaluation: %v", err)
		return
	}
	log.Printf("SYNC: star rules changed, re-evaluating")
	d.SetSyncState("star_rules", hash)
}
