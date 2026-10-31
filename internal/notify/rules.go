package notify

import (
	"errors"
	"fmt"
	"math"
	"slices"
)

// Rule IDs of the event rules.
const (
	RuleDeployFailed  = "deploy-failed"
	RuleBackupFailed  = "backup-failed"
	RuleRestoreFailed = "restore-failed"
	RuleCrashLoop     = "service-crash-loop"
	RuleOOM           = "service-oom"
)

// RuleDef is one entry of the catalog of things shed notifies about.
type RuleDef struct {
	ID          string
	Title       string
	Description string
	// Threshold is the default threshold, or nil if the rule has none.
	Threshold *float64
	// Unit labels the threshold, such as "crashes".
	Unit string
	// Whole means the threshold must be a whole number.
	Whole bool
	// ForSeconds is the default duration, or nil if the rule has none.
	ForSeconds *int
	// NeedsTelemetry means the rule reads metrics, so it works only while
	// telemetry is on.
	NeedsTelemetry bool
}

func ptr[T any](v T) *T { return &v }

// catalog lists every rule. All of them default to enabled.
var catalog = []RuleDef{
	{ID: RuleDeployFailed, Title: "Deploy failed", Description: "A deployment failed to go live."},
	{ID: RuleBackupFailed, Title: "Backup failed", Description: "A backup failed."},
	{ID: RuleRestoreFailed, Title: "Restore failed", Description: "A restore failed."},
	{ID: RuleCrashLoop, Title: "Service crash loop",
		Description: "A service crashed repeatedly within 10 minutes. Sent at most once every 30 minutes per service.",
		Threshold:   ptr(3.0), Unit: "crashes", Whole: true},
	{ID: RuleOOM, Title: "Service out of memory", Description: "A service was killed for running out of memory."},
}

// RuleOverride is a stored change to a rule's defaults. A nil Threshold or
// ForSeconds means the default.
type RuleOverride struct {
	ID         string
	Enabled    bool
	Threshold  *float64
	ForSeconds *int
}

// Rule is a catalog entry with its overrides applied.
type Rule struct {
	RuleDef
	Enabled bool
}

// MergeRules returns the catalog with overrides applied, in catalog order.
// A rule without an override has its defaults; overrides of unknown rules
// are ignored.
func MergeRules(overrides []RuleOverride) []Rule {
	out := make([]Rule, len(catalog))
	for i, d := range catalog {
		out[i] = Rule{RuleDef: d, Enabled: true}
		if j := slices.IndexFunc(overrides, func(o RuleOverride) bool { return o.ID == d.ID }); j >= 0 {
			o := overrides[j]
			out[i].Enabled = o.Enabled
			if d.Threshold != nil && o.Threshold != nil {
				out[i].Threshold = o.Threshold
			}
			if d.ForSeconds != nil && o.ForSeconds != nil {
				out[i].ForSeconds = o.ForSeconds
			}
		}
	}
	return out
}

// ErrUnknownRule is returned for a rule that is not in the catalog.
var ErrUnknownRule = errors.New("notify: unknown rule")

// ValidateRule reports whether threshold and forSeconds, when not nil, are
// allowed values for the rule id.
func ValidateRule(id string, threshold *float64, forSeconds *int) error {
	i := slices.IndexFunc(catalog, func(d RuleDef) bool { return d.ID == id })
	if i < 0 {
		return fmt.Errorf("%w %q", ErrUnknownRule, id)
	}
	d := catalog[i]
	if threshold != nil {
		switch v := *threshold; {
		case d.Threshold == nil:
			return fmt.Errorf("notify: rule %s has no threshold", id)
		case !(v > 0) || math.IsInf(v, 0):
			return errors.New("notify: threshold must be greater than 0")
		case d.Whole && (v != math.Trunc(v) || v < 1):
			return errors.New("notify: threshold must be a whole number of at least 1")
		}
	}
	if forSeconds != nil {
		switch {
		case d.ForSeconds == nil:
			return fmt.Errorf("notify: rule %s has no duration", id)
		case *forSeconds < 0:
			return errors.New("notify: forSeconds must not be negative")
		}
	}
	return nil
}
