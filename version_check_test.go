package vat

import (
	"context"
	"testing"
)

func TestParseMajorMinor(t *testing.T) {
	tests := []struct {
		version string
		major   int
		minor   int
		ok      bool
	}{
		{"9.13.2", 9, 13, true},
		{"9.14", 9, 14, true},
		{"9.14.0", 9, 14, true},
		{"9.14.0-SNAPSHOT", 9, 14, true},
		{"9.14-SNAPSHOT", 9, 14, true},
		{"9.14.0.1", 9, 14, true},
		{"v9.14.0", 9, 14, true},
		{"10.0", 10, 0, true},
		{"none_found", 0, 0, false},
		{"", 0, 0, false},
		{"9", 0, 0, false},
		{"dev-build", 0, 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			major, minor, ok := parseMajorMinor(tt.version)
			if ok != tt.ok || major != tt.major || minor != tt.minor {
				t.Errorf("parseMajorMinor(%q) = (%d, %d, %v), want (%d, %d, %v)", tt.version, major, minor, ok, tt.major, tt.minor, tt.ok)
			}
		})
	}
}

// TestCheckVectrVersionSupported asserts the vat 1.x range: VECTR must be
// below 9.14. Any 9.14 build (including SNAPSHOT/RC/four-part variants)
// counts as 9.14 and is rejected. Unparseable versions are allowed through
// with a warning.
func TestCheckVectrVersionSupported(t *testing.T) {
	tests := []struct {
		version string
		wantErr bool
	}{
		{"9.12.1", false},
		{"9.13.2", false},
		{"9.13.99-SNAPSHOT", false},
		{"9.14", true},
		{"9.14.0", true},
		{"9.14.0-SNAPSHOT", true},
		{"9.14-SNAPSHOT", true},
		{"9.14.0.1", true},
		{"9.15.1", true},
		{"10.0", true},
		{"none_found", false},
		{"garbage", false},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			err := CheckVectrVersionSupported(context.Background(), tt.version)
			if (err != nil) != tt.wantErr {
				t.Errorf("CheckVectrVersionSupported(%q) = %v, wantErr %v", tt.version, err, tt.wantErr)
			}
		})
	}
}

func TestVectrVersionRangeString(t *testing.T) {
	tests := []struct {
		r    vectrVersionRange
		want string
	}{
		{vectrVersionRange{Max: "9.14"}, "< 9.14"},
		{vectrVersionRange{Min: "9.14"}, ">= 9.14"},
		{vectrVersionRange{Min: "9.14", Max: "10.0"}, ">= 9.14 and < 10.0"},
		{vectrVersionRange{}, "any"},
	}

	for _, tt := range tests {
		if got := tt.r.String(); got != tt.want {
			t.Errorf("vectrVersionRange%+v.String() = %q, want %q", tt.r, got, tt.want)
		}
	}
}
