package config

import (
	"strings"
	"testing"
)

func TestTokens(t *testing.T) {
	for _, tc := range []struct {
		env, err string
		want     map[string]string
	}{
		{env: "kestra:aaaaaaaaaaaaaaaa,me:bbbbbbbbbbbbbbbb", want: map[string]string{"aaaaaaaaaaaaaaaa": "kestra", "bbbbbbbbbbbbbbbb": "me"}},
		{env: "kestra:aaaaaaaaaaaaaaaa\nme:bbbbbbbbbbbbbbbb\n", want: map[string]string{"aaaaaaaaaaaaaaaa": "kestra", "bbbbbbbbbbbbbbbb": "me"}},
		{env: "kestra:aaaaaaaaaaaaaaaame:bbbbbbbbbbbbbbbb", err: "colon"},
		{env: "kestra:aaaaaaaaaaaaaaaa me:bbbbbbbbbbbbbbbb", err: "colon"},
		{env: "kestra:short", err: "16 characters"},
		{env: "a:aaaaaaaaaaaaaaaa,b:aaaaaaaaaaaaaaaa", err: "twice"},
	} {
		t.Setenv("CHARTER_API_TOKENS", tc.env)
		got, err := Tokens()
		if tc.err != "" {
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("%q: err %v, want %q", tc.env, err, tc.err)
			}
			continue
		}
		if err != nil || len(got) != len(tc.want) {
			t.Fatalf("%q: %v %v", tc.env, got, err)
		}
		for k, v := range tc.want {
			if got[k] != v {
				t.Errorf("%q: %s -> %q, want %q", tc.env, k, got[k], v)
			}
		}
	}
}
