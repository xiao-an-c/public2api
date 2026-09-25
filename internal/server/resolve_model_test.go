package server

import "testing"

func TestResolveModelChannelPrefixes(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		wantRealm string
		wantBare  string
	}{
		{"bare defaults cn", "glm-5.2", "cn", "glm-5.2"},
		{"legacy cn", "cn:glm-5.2", "cn", "glm-5.2"},
		{"legacy global", "global:claude", "global", "claude"},
		{"channel cn", "wbp-cn:glm-5.2", "cn", "glm-5.2"},
		{"channel global", "wbp-global:claude", "global", "claude"},
		{"grok", "grok:grok-3", "grok", "grok-3"},
		{"unknown colon stays bare", "gpt-4:latest", "cn", "gpt-4:latest"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			realm, bare := ResolveModel(tc.input)
			if realm != tc.wantRealm || bare != tc.wantBare {
				t.Fatalf("ResolveModel(%q)=(%q,%q), want (%q,%q)", tc.input, realm, bare, tc.wantRealm, tc.wantBare)
			}
		})
	}
}
