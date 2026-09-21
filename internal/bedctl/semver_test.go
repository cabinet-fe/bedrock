package bedctl

import "testing"

func TestParseVersion(t *testing.T) {
	tests := []struct {
		in      string
		valid   bool
		majMiPa [3]int
		pre     string
	}{
		{"v2.5.9", true, [3]int{2, 5, 9}, ""},
		{"2.5.9", true, [3]int{2, 5, 9}, ""},
		{"v2.5.9-rc.1", true, [3]int{2, 5, 9}, "rc.1"},
		{"v2.5.9-rc.1+build.7", true, [3]int{2, 5, 9}, "rc.1"},
		{"dev", false, [3]int{0, 0, 0}, ""},
		{"unknown", false, [3]int{0, 0, 0}, ""},
		{"v2", true, [3]int{2, 0, 0}, ""}, // lenient: missing parts default to 0
		{"v2.x.1", false, [3]int{0, 0, 0}, ""},
		{"", false, [3]int{0, 0, 0}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			v := ParseVersion(tt.in)
			if v.Valid != tt.valid {
				t.Fatalf("ParseVersion(%q).Valid = %v, want %v", tt.in, v.Valid, tt.valid)
			}
			if v.Major != tt.majMiPa[0] || v.Minor != tt.majMiPa[1] || v.Patch != tt.majMiPa[2] {
				t.Fatalf("ParseVersion(%q) = %d.%d.%d, want %v", tt.in, v.Major, v.Minor, v.Patch, tt.majMiPa)
			}
			if v.Pre != tt.pre {
				t.Fatalf("ParseVersion(%q).Pre = %q, want %q", tt.in, v.Pre, tt.pre)
			}
		})
	}
}

func TestCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"v1.0.0", "v1.0.0", 0},
		{"v1.0.0", "v1.0.1", -1},
		{"v1.0.0", "v1.1.0", -1},
		{"v2.0.0", "v1.9.9", 1},
		{"v1.0.0-rc.1", "v1.0.0", -1},
		{"v1.0.0", "v1.0.0-rc.1", 1},
		{"v1.0.0-rc.1", "v1.0.0-rc.2", -1},
		{"v1.0.0-rc.1", "v1.0.0-beta", 1}, // numeric > alphanumeric per semver 11
		{"dev", "v1.0.0", -1},             // invalid sorts below valid
		{"v1.0.0", "dev", 1},
		{"unknown", "garbage", 1},     // invalid vs invalid: raw string compare
		{"v1.0.0+build", "v1.0.0", 0}, // build metadata ignored
	}
	for _, tt := range tests {
		t.Run(tt.a+" vs "+tt.b, func(t *testing.T) {
			if got := Compare(ParseVersion(tt.a), ParseVersion(tt.b)); got != tt.want {
				t.Fatalf("Compare(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestNeedsUpdate(t *testing.T) {
	tests := []struct {
		current, target string
		skip            bool
	}{
		{"v2.5.9", "v2.5.9", true},
		{"v2.5.8", "v2.5.9", false},
		{"v2.5.10", "v2.5.9", true},
		{"unknown", "v2.5.9", false}, // failed --version read counts as outdated
		{"dev", "v2.5.9", false},
		{"v2.5.9-rc.1", "v2.5.9", false},
	}
	for _, tt := range tests {
		t.Run(tt.current+"→"+tt.target, func(t *testing.T) {
			skip, _ := NeedsUpdate(tt.current, tt.target)
			if skip != tt.skip {
				t.Fatalf("NeedsUpdate(%q, %q) skip = %v, want %v", tt.current, tt.target, skip, tt.skip)
			}
		})
	}
}
