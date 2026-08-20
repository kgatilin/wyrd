package overlay

import "strings"

// Matches reports whether pkg satisfies the selector: included by at least
// one Include pattern (an empty Include list includes everything) and not
// matched by any Exclude pattern.
//
// The pattern syntax is the one documented on PackageSelector: "*" matches a
// single path segment and "..." matches the remaining segments. It lives here,
// next to the type it interprets, so every consumer of a PackageSelector — the
// review projection, the review report — resolves a package the same way.
func (s PackageSelector) Matches(pkg string) bool {
	included := len(s.Include) == 0
	for _, pattern := range s.Include {
		if matchPackagePattern(pattern, pkg) {
			included = true
			break
		}
	}
	if !included {
		return false
	}
	for _, pattern := range s.Exclude {
		if matchPackagePattern(pattern, pkg) {
			return false
		}
	}
	return true
}

// ReviewGroupOf resolves which configured review group owns pkg. Groups are
// evaluated in lexical key order and the first match wins; child is the direct
// sub-directory the group's per_directory split assigns the package to, empty
// when the group does not split or pkg sits at the prefix itself. ok is false
// when no group matches (or no groups are configured), which leaves the caller
// free to name the fallback bucket in its own vocabulary.
func (c *Config) ReviewGroupOf(pkg string) (name, child string, ok bool) {
	if c == nil || len(c.ReviewGroups) == 0 {
		return "", "", false
	}
	for _, key := range sortedKeys(c.ReviewGroups) {
		group := c.ReviewGroups[key]
		if !group.Packages.Matches(pkg) {
			continue
		}
		return key, perDirectoryChild(group.PerDirectory, pkg), true
	}
	return "", "", false
}

// perDirectoryChild returns the direct child segment of pkg under prefix, or
// "" when the group has no per_directory split or pkg is not strictly below
// the prefix (including pkg == prefix, which stays in the base group).
func perDirectoryChild(prefix, pkg string) string {
	prefix = NormalizePackagePath(prefix)
	if prefix == "" {
		return ""
	}
	pkg = NormalizePackagePath(pkg)
	if !strings.HasPrefix(pkg, prefix+"/") {
		return ""
	}
	rest := pkg[len(prefix)+1:]
	if slash := strings.IndexByte(rest, '/'); slash >= 0 {
		rest = rest[:slash]
	}
	return rest
}

func matchPackagePattern(pattern, pkg string) bool {
	pattern = NormalizePackagePath(pattern)
	pkg = NormalizePackagePath(pkg)
	if pattern == "" {
		return pkg == ""
	}
	return matchSegments(strings.Split(pattern, "/"), strings.Split(pkg, "/"))
}

func matchSegments(pattern, pkg []string) bool {
	for len(pattern) > 0 {
		head := pattern[0]
		pattern = pattern[1:]
		if head == "..." {
			return len(pattern) == 0
		}
		if len(pkg) == 0 {
			return false
		}
		if head != "*" && head != pkg[0] {
			return false
		}
		pkg = pkg[1:]
	}
	return len(pkg) == 0
}

// NormalizePackagePath trims a package path to the module-relative,
// forward-slashed, unrooted form selectors are written in.
func NormalizePackagePath(p string) string {
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	p = strings.TrimPrefix(p, "./")
	if p == "." {
		return ""
	}
	return strings.Trim(p, "/")
}
