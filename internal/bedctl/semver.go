package bedctl

import (
	"fmt"
	"strings"
)

// Version is a parsed semver-ish release tag (v-prefix optional). Pre-release
// and build metadata are handled per semver; unparsable tags sort below any
// parsable one so "dev" or garbage never looks newer than a release.
type Version struct {
	Major, Minor, Patch int
	Pre                 string // semver pre-release, compares lower than none
	Build               string // ignored for ordering
	Valid               bool
	Raw                 string
}

// ParseVersion parses a tag like v2.5.9-rc.1. Leading "v" is optional.
func ParseVersion(s string) Version {
	raw := strings.TrimSpace(s)
	v := Version{Raw: raw}
	tag := strings.TrimPrefix(raw, "v")
	if i := strings.IndexByte(tag, '+'); i >= 0 {
		v.Build = tag[i+1:]
		tag = tag[:i]
	}
	if i := strings.IndexByte(tag, '-'); i >= 0 {
		v.Pre = tag[i+1:]
		tag = tag[:i]
	}
	parts := strings.Split(tag, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return v
	}
	nums := make([]int, 3)
	for i, p := range parts {
		if p == "" || len(p) > 9 {
			return v
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return v
			}
		}
		n := 0
		for _, r := range p {
			n = n*10 + int(r-'0')
		}
		nums[i] = n
	}
	v.Major, v.Minor, v.Patch = nums[0], nums[1], nums[2]
	v.Valid = true
	return v
}

// String renders the parsed version back to a tag form (v-prefix kept off
// Build/Pre formatting; Raw is what callers usually want to display).
func (v Version) String() string { return v.Raw }

// Compare returns -1, 0 or 1. Invalid versions sort below valid ones;
// two invalid versions compare by raw string.
func Compare(a, b Version) int {
	switch {
	case a.Valid && !b.Valid:
		return 1
	case !a.Valid && b.Valid:
		return -1
	case !a.Valid && !b.Valid:
		return strings.Compare(a.Raw, b.Raw)
	}
	if c := compareInt(a.Major, b.Major); c != 0 {
		return c
	}
	if c := compareInt(a.Minor, b.Minor); c != 0 {
		return c
	}
	if c := compareInt(a.Patch, b.Patch); c != 0 {
		return c
	}
	switch {
	case a.Pre == "" && b.Pre == "":
		return 0
	case a.Pre == "":
		return 1 // release beats pre-release
	case b.Pre == "":
		return -1
	}
	return comparePre(a.Pre, b.Pre)
}

func compareInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// comparePre compares dot-separated pre-release identifiers per semver rule 11:
// numeric identifiers compare numerically and below alphanumeric ones.
func comparePre(a, b string) int {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	for i := range min(len(as), len(bs)) {
		an, aok := numericID(as[i])
		bn, bok := numericID(bs[i])
		switch {
		case aok && bok:
			if c := compareInt(an, bn); c != 0 {
				return c
			}
		case aok:
			return -1
		case bok:
			return 1
		default:
			if c := strings.Compare(as[i], bs[i]); c != 0 {
				return c
			}
		}
	}
	return compareInt(len(as), len(bs))
}

func numericID(s string) (int, bool) {
	if s == "" || (len(s) > 1 && s[0] == '0') {
		return 0, false
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, true
}

// NeedsUpdate decides whether an installed binary (version string current,
// possibly "unknown" or "dev") should be replaced by release tag target.
// skip is true when already up to date; note explains the decision for
// display. An unparsable target falls back to exact string equality, the
// only safe comparison; anything unparsable on the installed side counts as
// outdated so a failed --version read no longer silently reinstalls forever
// (the old shell behavior) but a genuinely older version still upgrades.
func NeedsUpdate(current, target string) (skip bool, note string) {
	cur := ParseVersion(current)
	tgt := ParseVersion(target)
	if !tgt.Valid {
		if strings.TrimSpace(current) == strings.TrimSpace(target) {
			return true, "已与目标版本一致"
		}
		return false, fmt.Sprintf("目标版本 %q 无法按语义化版本解析，按需重新安装", target)
	}
	if !cur.Valid {
		return false, fmt.Sprintf("当前版本 %q 无法识别，将安装 %s", current, target)
	}
	if Compare(cur, tgt) >= 0 {
		return true, fmt.Sprintf("已是最新版本（%s）", current)
	}
	return false, fmt.Sprintf("%s → %s", current, target)
}
