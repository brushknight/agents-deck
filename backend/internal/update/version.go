package update

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Version is a semantic version (https://semver.org): MAJOR.MINOR.PATCH, an
// optional pre-release after "-" and build metadata after "+" (ignored when
// comparing). A leading "v" is accepted.
type Version struct {
	Major, Minor, Patch uint64
	Pre                 []string // dot-separated pre-release identifiers
	Build               string
}

func ParseVersion(s string) (Version, error) {
	var v Version
	t := strings.TrimPrefix(strings.TrimSpace(s), "v")
	t, v.Build, _ = strings.Cut(t, "+")
	core, pre, hasPre := strings.Cut(t, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return v, fmt.Errorf("version %q: want MAJOR.MINOR.PATCH", s)
	}
	nums := [3]*uint64{&v.Major, &v.Minor, &v.Patch}
	for i, p := range parts {
		if !numeric(p) || (len(p) > 1 && p[0] == '0') {
			return v, fmt.Errorf("version %q: %q isn't a number", s, p)
		}
		n, err := strconv.ParseUint(p, 10, 64)
		if err != nil {
			return v, fmt.Errorf("version %q: %w", s, err)
		}
		*nums[i] = n
	}
	if hasPre {
		v.Pre = strings.Split(pre, ".")
		for _, id := range v.Pre {
			if id == "" || !identRe.MatchString(id) || (numeric(id) && len(id) > 1 && id[0] == '0') {
				return v, fmt.Errorf("version %q: bad pre-release %q", s, pre)
			}
		}
	}
	return v, nil
}

var identRe = regexp.MustCompile(`^[0-9A-Za-z-]+$`)

func numeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func (v Version) String() string {
	s := fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
	if len(v.Pre) > 0 {
		s += "-" + strings.Join(v.Pre, ".")
	}
	if v.Build != "" {
		s += "+" + v.Build
	}
	return s
}

// Compare is -1, 0 or 1 by semver precedence: a pre-release comes before its
// release (0.2.0-rc.1 < 0.2.0), numeric identifiers compare as numbers and
// before alphanumeric ones.
func (v Version) Compare(w Version) int {
	for _, d := range [][2]uint64{{v.Major, w.Major}, {v.Minor, w.Minor}, {v.Patch, w.Patch}} {
		if d[0] != d[1] {
			if d[0] < d[1] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(v.Pre) == 0 && len(w.Pre) == 0:
		return 0
	case len(v.Pre) == 0:
		return 1
	case len(w.Pre) == 0:
		return -1
	}
	for i := 0; i < len(v.Pre) && i < len(w.Pre); i++ {
		a, b := v.Pre[i], w.Pre[i]
		if a == b {
			continue
		}
		an, bn := numeric(a), numeric(b)
		switch {
		case an && bn:
			x, _ := strconv.ParseUint(a, 10, 64)
			y, _ := strconv.ParseUint(b, 10, 64)
			if x < y {
				return -1
			}
			return 1
		case an:
			return -1
		case bn:
			return 1
		case a < b:
			return -1
		default:
			return 1
		}
	}
	switch {
	case len(v.Pre) < len(w.Pre):
		return -1
	case len(v.Pre) > len(w.Pre):
		return 1
	}
	return 0
}

// Newer reports whether candidate is a newer version than current. Anything
// that doesn't parse is never newer.
func Newer(candidate, current string) bool {
	c, err := ParseVersion(candidate)
	if err != nil {
		return false
	}
	cur, err := ParseVersion(current)
	if err != nil {
		return false
	}
	return c.Compare(cur) > 0
}

// describeRe matches what `git describe` adds after a tag: -<n>-g<hash>.
var describeRe = regexp.MustCompile(`-\d+-g[0-9a-f]{4,}`)

// IsDevBuild: a version that isn't a release's. Release builds are stamped
// with the bare version ("0.2.0"); source builds with "dev" or `git describe`
// output ("v0.2.0-3-gabc1234-dirty").
func IsDevBuild(version string) bool {
	if version == "" || version == "dev" || strings.HasPrefix(version, "v") ||
		strings.Contains(version, "dirty") || describeRe.MatchString(version) {
		return true
	}
	_, err := ParseVersion(version)
	return err != nil
}
