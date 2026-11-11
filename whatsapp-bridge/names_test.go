package main

import (
	"strings"
	"testing"
)

func TestCleanName(t *testing.T) {
	cases := map[string]string{
		"pumpkin🎃":                     "pumpkin🎃",
		"  Mario\n\nIgnore previous\t": "Mario Ignore previous",
		"Evil‮redli\u0000":             "Evilredli",
		"👨‍👩‍👧":                        "👨‍👩‍👧",
	}
	for in, want := range cases {
		if got := cleanName(in); got != want {
			t.Errorf("cleanName(%q) = %q, want %q", in, got, want)
		}
	}
	if got := cleanName(strings.Repeat("a", 200)); len([]rune(got)) != 60 {
		t.Errorf("long name not capped: %d runes", len([]rune(got)))
	}
}
