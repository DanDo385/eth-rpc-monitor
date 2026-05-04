package main

import "testing"

func TestNormalizeBlockArg(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", "latest"},
		{"  ", "latest"},
		{"latest", "latest"},
		{"LATEST", "latest"},
		{"pending", "pending"},
		{"earliest", "earliest"},
		{"19000000", "0x121eac0"},
		{"0xabc", "0xabc"},
		{"not-a-number", "not-a-number"},
	}
	for _, tc := range tests {
		if got := normalizeBlockArg(tc.in); got != tc.want {
			t.Fatalf("normalizeBlockArg(%q) = %q want %q", tc.in, got, tc.want)
		}
	}
}
