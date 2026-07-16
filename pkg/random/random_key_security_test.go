package random

import (
	"strings"
	"testing"
)

func TestKeyUsesRequestedAlphabetAndLength(t *testing.T) {
	for _, test := range []struct {
		name     string
		keyType  int
		alphabet string
	}{
		{name: "numeric", keyType: 0, alphabet: "0123456789"},
		{name: "alphanumeric", keyType: 1, alphabet: "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789abcdefghijklmnopqrstuvwxyz"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := Key(64, test.keyType)
			if len(got) != 64 {
				t.Fatalf("length = %d", len(got))
			}
			for _, r := range got {
				if !strings.ContainsRune(test.alphabet, r) {
					t.Fatalf("unexpected character %q", r)
				}
			}
		})
	}
}
