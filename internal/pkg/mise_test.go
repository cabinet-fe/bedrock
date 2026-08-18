package pkg

import (
	"fmt"
	"strings"
	"testing"
)

func TestParseVersionLines(t *testing.T) {
	raw := "mise WARN x\n# comment\n1.0.0 extra\n1.0.0\nerror boom\n2.0.0\n"
	got := ParseVersionLines(raw, 10)
	if strings.Join(got, ",") != "2.0.0,1.0.0" {
		t.Fatalf("got %#v", got)
	}
	var numbered strings.Builder
	for i := 1; i <= 8; i++ {
		fmt.Fprintf(&numbered, "1.0.%d\n", i)
	}
	got = ParseVersionLines(numbered.String(), 5)
	if strings.Join(got, ",") != "1.0.8,1.0.7,1.0.6,1.0.5,1.0.4" {
		t.Fatalf("limit newest-first = %#v", got)
	}
}
