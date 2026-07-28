package vat

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
)

// vectrVersionRange describes the range of VECTR versions supported by this
// major version of vat. Bounds are expressed at major.minor granularity;
// an empty bound means unbounded on that side.
type vectrVersionRange struct {
	Min string // inclusive lower bound, e.g. "9.14"; "" means no lower bound
	Max string // exclusive upper bound, e.g. "9.14"; "" means no upper bound
}

// String describes the range in a human-readable form for error messages.
func (r vectrVersionRange) String() string {
	switch {
	case r.Min != "" && r.Max != "":
		return fmt.Sprintf(">= %s and < %s", r.Min, r.Max)
	case r.Min != "":
		return fmt.Sprintf(">= %s", r.Min)
	case r.Max != "":
		return fmt.Sprintf("< %s", r.Max)
	default:
		return "any"
	}
}

var majorMinorRegexp = regexp.MustCompile(`^v?(\d+)\.(\d+)`)

// parseMajorMinor extracts the leading major and minor components from a
// version string. VECTR versions are maven-style rather than strict semver,
// so this tolerates qualifiers and extra components (e.g. 9.14.0-SNAPSHOT,
// 9.14-RC1, 9.14.0.1); anything after major.minor is ignored.
func parseMajorMinor(version string) (major, minor int, ok bool) {
	m := majorMinorRegexp.FindStringSubmatch(version)
	if m == nil {
		return 0, 0, false
	}
	major, majErr := strconv.Atoi(m[1])
	minor, minErr := strconv.Atoi(m[2])
	if majErr != nil || minErr != nil {
		return 0, 0, false
	}
	return major, minor, true
}

// CheckVectrVersionSupported verifies that the live VECTR version falls
// within the range supported by this major version of vat.
//
// The comparison happens at major.minor granularity, so any 9.14 build
// (9.14, 9.14.0, 9.14.0-SNAPSHOT, ...) is treated as 9.14. A version string
// that does not start with a parseable major.minor (e.g. a dev build) is
// logged as a warning and allowed through rather than blocking the run.
//
// Returns an error describing the supported range if the version is outside
// it, nil otherwise.
func CheckVectrVersionSupported(ctx context.Context, vectrVersion string) error {
	major, minor, ok := parseMajorMinor(vectrVersion)
	if !ok {
		slog.WarnContext(ctx, "could not parse VECTR version, skipping version compatibility check", "vectr-version", vectrVersion)
		return nil
	}

	if supportedVectrRange.Min != "" {
		minMajor, minMinor, ok := parseMajorMinor(supportedVectrRange.Min)
		if ok && (major < minMajor || (major == minMajor && minor < minMinor)) {
			return rangeError(vectrVersion)
		}
	}

	if supportedVectrRange.Max != "" {
		maxMajor, maxMinor, ok := parseMajorMinor(supportedVectrRange.Max)
		if ok && (major > maxMajor || (major == maxMajor && minor >= maxMinor)) {
			return rangeError(vectrVersion)
		}
	}

	return nil
}

// rangeError builds the error returned when a VECTR version falls outside
// the supported range.
func rangeError(vectrVersion string) error {
	return fmt.Errorf("VECTR version %q is outside the range supported by this version of vat (%s): %s", vectrVersion, supportedVectrRange, versionCheckAdvice)
}
