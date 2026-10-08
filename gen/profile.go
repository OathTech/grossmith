package gen

import (
	"fmt"
	"sort"

	"grossmith/internal/strictjson"
)

// Named construct profiles (gc-verified arc G2; their R2). A profile is an
// ALLOW-LIST of optional construct tags: every optional tag it does not
// list is excluded. A tag added to grossmith later is therefore excluded
// from every existing profile until the profile's owner lists it, which is
// the safe direction for a consumer that certifies by fragment.
//
// Profiles are owned by their consumer (gc-verified owns its fragment
// files); grossmith provides the mechanism and guarantees:
//
//   - by construction, a disabled construct is never emitted (mark panics
//     otherwise), and swarm sampling still varies the mix within the
//     allow-list;
//   - CheckFeatures refuses a case whose subject or battery functions
//     recorded an optional tag outside the allow-list, as a second line;
//   - the profile is recorded with every case (name and sha256) and its
//     exclusions are re-derived by gengo -verify.
//
// What a tag admits at source level is grossmith's tag semantics, not the
// consumer's fragment rules: a consumer checks that mapping with its own
// classifier.

// ProfileSchema is the profile file format.
const ProfileSchema = "grossmith-profile-v1"

// Profile is a parsed profile file.
type Profile struct {
	Schema string `json:"schema"`
	Name   string `json:"name"`
	// Description is free text for the profile's owner.
	Description string `json:"description,omitempty"`
	// Constructs is the allow-list of optional construct tags.
	Constructs []string `json:"constructs"`
}

// ParseProfile strictly decodes and validates a profile file.
func ParseProfile(b []byte) (Profile, error) {
	var p Profile
	if err := strictjson.Unmarshal(b, &p); err != nil {
		return Profile{}, fmt.Errorf("profile: %w", err)
	}
	if p.Schema != ProfileSchema {
		return Profile{}, fmt.Errorf("profile: schema %q, want %q", p.Schema, ProfileSchema)
	}
	if !profileNameOK(p.Name) {
		return Profile{}, fmt.Errorf("profile: name %q must be 1-64 characters from [A-Za-z0-9._-]", p.Name)
	}
	if p.Constructs == nil {
		// An empty allow-list is legal (core constructs only); an absent
		// one is more likely a misspelled field.
		return Profile{}, fmt.Errorf("profile %s: constructs is missing (use [] for core constructs only)", p.Name)
	}
	known := map[string]bool{}
	for _, t := range append(Optional(), OptIn()...) {
		known[t] = true
	}
	seen := map[string]bool{}
	for _, t := range p.Constructs {
		switch {
		case coreConstructs[t]:
			return Profile{}, fmt.Errorf("profile %s: %q is a core construct, always enabled; do not list it", p.Name, t)
		case !known[t]:
			return Profile{}, fmt.Errorf("profile %s: unknown construct tag %q", p.Name, t)
		case seen[t]:
			return Profile{}, fmt.Errorf("profile %s: construct %q listed twice", p.Name, t)
		}
		seen[t] = true
	}
	return p, nil
}

func profileNameOK(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	// Starts with a letter or digit, so "." and ".." are not names.
	if c := s[0]; !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
		return false
	}
	for _, r := range s {
		ok := r == '.' || r == '_' || r == '-' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		if !ok {
			return false
		}
	}
	return true
}

// Allows reports whether the profile admits an optional construct tag.
func (p Profile) Allows(tag string) bool {
	for _, t := range p.Constructs {
		if t == tag {
			return true
		}
	}
	return false
}

// Excluded is every optional tag the profile does not list, in Optional()
// order.
func (p Profile) Excluded() []string {
	var out []string
	for _, t := range Optional() {
		if !p.Allows(t) {
			out = append(out, t)
		}
	}
	return out
}

// Apply returns cfg with the profile's exclusions added to cfg.Exclude
// (a union: an adapter profile's own exclusions are kept), in Optional()
// order so the recorded config does not depend on input order.
func (p Profile) Apply(cfg Config) Config {
	ex := map[string]bool{}
	for _, t := range cfg.Exclude {
		ex[t] = true
	}
	for _, t := range p.Excluded() {
		ex[t] = true
	}
	var out []string
	for _, t := range Optional() {
		if ex[t] {
			out = append(out, t)
			delete(ex, t)
		}
	}
	// Opt-in tags the profile lists are requested through Include.
	for _, t := range OptIn() {
		if p.Allows(t) && !containsTag(cfg.Include, t) {
			cfg.Include = append(cfg.Include, t)
		}
	}
	// Anything outside Optional() is kept for Validate to refuse.
	var rest []string
	for t := range ex {
		rest = append(rest, t)
	}
	sort.Strings(rest)
	cfg.Exclude = append(out, rest...)
	return cfg
}

// CheckFeatures refuses a case that recorded an optional construct tag the
// profile does not allow, in its subject or any battery function. Info
// tags and core constructs are not constructs a profile gates.
func (p Profile) CheckFeatures(c Case) error {
	known := map[string]bool{}
	for _, t := range append(Optional(), OptIn()...) {
		known[t] = true
	}
	check := func(where string, tags map[string]int) error {
		for t := range tags {
			if known[t] && !p.Allows(t) {
				return fmt.Errorf("profile %s: %s emitted construct %q, which the profile does not allow (generator bug)", p.Name, where, t)
			}
		}
		return nil
	}
	if err := check("the subject", c.FeatureCounts); err != nil {
		return err
	}
	for _, f := range c.Functions {
		if err := check(f.Name, f.Features); err != nil {
			return err
		}
	}
	return nil
}
