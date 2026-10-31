// Copyright 2026 Leadlight Authors
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"leadlight/gitops"
	"time"
)

// HistoryLimit controls how far back to fetch historical patches.
// Uses calendar units for correct date math (e.g., "1y" from March
// 2026 = March 2025, handling leap years and varying month lengths).
type HistoryLimit struct {
	Years  int
	Months int
	Days   int
}

func (h HistoryLimit) IsZero() bool {
	return h.Years == 0 && h.Months == 0 && h.Days == 0
}

// Before returns the cutoff time for history backfill.
func (h HistoryLimit) Before() time.Time {
	return time.Now().UTC().AddDate(-h.Years, -h.Months, -h.Days)
}

// ParseHistoryLimit parses a duration string like "30d", "4w", "6mo",
// "1y" into calendar components. Returns zero value for "" and "0d".
func ParseHistoryLimit(s string) (HistoryLimit, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" || s == "0" || s == "0d" {
		return HistoryLimit{}, nil
	}
	if strings.HasSuffix(s, "y") {
		n, err := strconv.Atoi(s[:len(s)-1])
		if err != nil || n < 0 {
			return HistoryLimit{}, fmt.Errorf(
				"invalid history limit: %q", s)
		}
		return HistoryLimit{Years: n}, nil
	}
	if strings.HasSuffix(s, "mo") {
		n, err := strconv.Atoi(s[:len(s)-2])
		if err != nil || n < 0 {
			return HistoryLimit{}, fmt.Errorf(
				"invalid history limit: %q", s)
		}
		return HistoryLimit{Months: n}, nil
	}
	if strings.HasSuffix(s, "w") {
		n, err := strconv.Atoi(s[:len(s)-1])
		if err != nil || n < 0 {
			return HistoryLimit{}, fmt.Errorf(
				"invalid history limit: %q", s)
		}
		return HistoryLimit{Days: n * 7}, nil
	}
	if strings.HasSuffix(s, "d") {
		n, err := strconv.Atoi(s[:len(s)-1])
		if err != nil || n < 0 {
			return HistoryLimit{}, fmt.Errorf(
				"invalid history limit: %q", s)
		}
		return HistoryLimit{Days: n}, nil
	}
	return HistoryLimit{}, fmt.Errorf(
		"invalid history limit: %q (use Nd, Nw, Nmo, or Ny)", s)
}

type Config struct {
	Server           string
	Project          string
	Token            string
	Username         string
	Password         string
	DBPath           string
	States           []string
	LoreURL          string
	MailArchive      string
	Theme            string
	BaseURL          string
	APIVersion       string
	HistoryLimit     HistoryLimit
	Signoff          *bool // nil = default (true), false = don't add -s to git am
	FixGmailWrapping *bool // nil = default (true), false = disable quote rejoin

	// Auto-star rules.  Each is a list of case-insensitive
	// substrings; a patch matching any entry is starred.
	StarEmails     []string // matched against the To and Cc headers
	StarKeywords   []string // matched against the subject
	StarSubmitters []string // matched against submitter name or email
	AutoStar       *bool    // nil = default (true), false = no auto-starring
}

// StarRules reports whether any auto-star rule is configured and
// enabled.
func (c *Config) StarRules() bool {
	if c.AutoStar != nil && !*c.AutoStar {
		return false
	}
	return len(c.StarEmails) > 0 || len(c.StarKeywords) > 0 ||
		len(c.StarSubmitters) > 0
}

// StarRulesHash is a stable fingerprint of the active rule set.  A
// change means every rule-derived star has to be recomputed, so the
// value is recorded in the database and compared on startup.
func (c *Config) StarRulesHash() string {
	if !c.StarRules() {
		return "none"
	}
	parts := make([]string, 0,
		len(c.StarEmails)+len(c.StarKeywords)+len(c.StarSubmitters))
	for _, v := range c.StarEmails {
		parts = append(parts, "e:"+strings.ToLower(v))
	}
	for _, v := range c.StarKeywords {
		parts = append(parts, "k:"+strings.ToLower(v))
	}
	for _, v := range c.StarSubmitters {
		parts = append(parts, "s:"+strings.ToLower(v))
	}
	sort.Strings(parts)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return hex.EncodeToString(sum[:8])
}

func Load(dir string, local bool) (*Config, error) {
	cfg := &Config{
		Server:      getWithFallback(dir, "leadlight.server", "pw.server", local),
		Project:     getWithFallback(dir, "leadlight.project", "pw.project", local),
		Token:       getWithFallback(dir, "leadlight.token", "pw.token", local),
		Username:    getWithFallback(dir, "leadlight.username", "pw.username", local),
		Password:    getWithFallback(dir, "leadlight.password", "pw.password", local),
		DBPath:      gitops.ConfigGet(dir, "leadlight.db", local),
		LoreURL:     gitops.ConfigGet(dir, "leadlight.lore", local),
		MailArchive: gitops.ConfigGet(dir, "leadlight.mailarchive", local),
		Theme:       gitops.ConfigGet(dir, "leadlight.theme", local),
	}

	if cfg.DBPath == "" {
		gd := gitops.CommonDir(dir)
		if gd != "" {
			llDir := filepath.Join(gd, "leadlight")
			os.MkdirAll(llDir, 0755)
			cfg.DBPath = filepath.Join(llDir, "leadlight.db")
		} else {
			cfg.DBPath = ".leadlight.db"
		}
	}

	rawStates := getWithFallback(dir, "leadlight.states", "pw.states", local)
	cfg.States = parseStates(rawStates)

	cfg.BaseURL = deriveBaseURL(cfg.Server)
	cfg.APIVersion = parseAPIVersion(cfg.Server)

	historyStr := gitops.ConfigGet(dir, "leadlight.history", local)
	if historyStr != "" {
		limit, err := ParseHistoryLimit(historyStr)
		if err != nil {
			return nil, err
		}
		cfg.HistoryLimit = limit
	}

	signoffStr := gitops.ConfigGet(dir, "leadlight.signoff", local)
	if signoffStr == "false" {
		f := false
		cfg.Signoff = &f
	}

	fixWrapStr := gitops.ConfigGet(dir, "leadlight.fix-gmail-wrapping", local)
	if fixWrapStr == "false" {
		f := false
		cfg.FixGmailWrapping = &f
	}

	autoStarStr := gitops.ConfigGet(dir, "leadlight.autostar", local)
	if autoStarStr == "false" {
		f := false
		cfg.AutoStar = &f
	}

	cfg.StarEmails = gitops.ConfigGetAll(dir, "leadlight.star-email", local)
	cfg.StarKeywords = gitops.ConfigGetAll(dir, "leadlight.star-keyword", local)
	cfg.StarSubmitters = gitops.ConfigGetAll(
		dir, "leadlight.star-submitter", local)

	// Being on Cc is the rule nearly everyone wants, so it needs no
	// configuration: fall back to the address git already knows.
	if len(cfg.StarEmails) == 0 {
		if email := gitops.UserEmail(dir); email != "" {
			cfg.StarEmails = []string{email}
		}
	}

	if err := validate(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

func getWithFallback(dir, primary, fallback string, local bool) string {
	if v := gitops.ConfigGet(dir, primary, local); v != "" {
		return v
	}
	return gitops.ConfigGet(dir, fallback, local)
}

func parseStates(raw string) []string {
	if raw == "" {
		return []string{"new", "under-review"}
	}
	parts := strings.Split(raw, ",")
	states := make([]string, 0, len(parts))
	for _, p := range parts {
		s := strings.TrimSpace(p)
		if s != "" {
			states = append(states, s)
		}
	}
	if len(states) == 0 {
		return []string{"new", "under-review"}
	}
	return states
}

var apiPathRe = regexp.MustCompile(`/api/[\d.]+/?$`)

func deriveBaseURL(server string) string {
	loc := apiPathRe.FindStringIndex(server)
	if loc == nil {
		return server
	}
	return server[:loc[0]]
}

var apiVersionRe = regexp.MustCompile(`/api/([\d.]+)/?$`)

func parseAPIVersion(server string) string {
	m := apiVersionRe.FindStringSubmatch(server)
	if m == nil {
		return ""
	}
	return m[1]
}

func validate(cfg *Config) error {
	var missing []string
	if cfg.Server == "" {
		missing = append(missing, "server (pw.server or leadlight.server)")
	}
	if cfg.Project == "" {
		missing = append(missing, "project (pw.project or leadlight.project)")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required config: %s", strings.Join(missing, ", "))
	}
	if cfg.APIVersion == "" || cfg.APIVersion < "1.2" {
		return fmt.Errorf("unsupported API version %q (need >= 1.2);"+
			" server URL should end with /api/1.2 or /api/1.3", cfg.APIVersion)
	}
	if cfg.APIVersion < "1.3" && cfg.MailArchive == "" {
		return fmt.Errorf("API %s lacks comment events;"+
			" set leadlight.mailarchive to enable comment tracking", cfg.APIVersion)
	}
	rules := map[string][]string{
		"leadlight.star-email":     cfg.StarEmails,
		"leadlight.star-keyword":   cfg.StarKeywords,
		"leadlight.star-submitter": cfg.StarSubmitters,
	}
	for _, key := range sortedKeys(rules) {
		for _, v := range rules[key] {
			if strings.TrimSpace(v) == "" {
				return fmt.Errorf("empty value in %s", key)
			}
		}
	}
	return nil
}

func sortedKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
