package main

import (
	"reflect"
	"testing"
)

func TestParseMethods(t *testing.T) {
	all := map[string]bool{"google": true, "yandex": true, "telegram": true, "solana": true}
	cases := []struct {
		in   string
		want map[string]bool
	}{
		{"", all},
		{"google,yandex,solana", map[string]bool{"google": true, "yandex": true, "solana": true}},
		{" google , telegram ,solana", map[string]bool{"google": true, "telegram": true, "solana": true}},
	}
	for _, c := range cases {
		got, err := parseMethods(c.in)
		if err != nil {
			t.Fatalf("parseMethods(%q): %v", c.in, err)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Fatalf("parseMethods(%q) = %v, want %v", c.in, got, c.want)
		}
	}

	for _, bad := range []string{"google,tg", ",", "Google"} {
		if _, err := parseMethods(bad); err == nil {
			t.Fatalf("parseMethods(%q) accepted", bad)
		}
	}
}
