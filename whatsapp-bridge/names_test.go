package main

import (
	"strings"
	"testing"

	"go.mau.fi/whatsmeow/types"
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

func TestNeedsNameLookup(t *testing.T) {
	jid := types.NewJID("12345", types.HiddenUserServer)
	for name, want := range map[string]bool{"": true, "12345": true, "~Mamma": true, "Filo Materia": false} {
		if got := needsNameLookup(name, jid); got != want {
			t.Errorf("needsNameLookup(%q) = %v, want %v", name, got, want)
		}
	}
}
