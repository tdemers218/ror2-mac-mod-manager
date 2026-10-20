// Package semver compares the simple major.minor.patch versions Thunderstore uses.
package semver

import (
	"strconv"
	"strings"
)

// Parse splits "1.2.3" into numbers. Missing parts count as 0.
func Parse(s string) ([3]int, bool) {
	var v [3]int
	parts := strings.Split(strings.TrimSpace(s), ".")
	if len(parts) == 0 || len(parts) > 4 {
		return v, false
	}
	for i := 0; i < len(parts) && i < 3; i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

// Compare returns -1, 0 or 1. Unparseable versions fall back to string order.
func Compare(a, b string) int {
	va, okA := Parse(a)
	vb, okB := Parse(b)
	if !okA || !okB {
		return strings.Compare(a, b)
	}
	for i := 0; i < 3; i++ {
		if va[i] < vb[i] {
			return -1
		}
		if va[i] > vb[i] {
			return 1
		}
	}
	return 0
}

// Less reports whether a is an older version than b.
func Less(a, b string) bool { return Compare(a, b) < 0 }
