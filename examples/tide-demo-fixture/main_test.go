// SPDX-License-Identifier: MIT
// Copyright (c) 2026 The TIDE Authors

package main

import "testing"

func TestGreeting(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "alice", in: "Alice", want: "Hello, Alice!"},
		{name: "world", in: "world", want: "Hello, world!"},
		{name: "empty", in: "", want: "Hello, !"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := Greeting(tc.in)
			if got != tc.want {
				t.Errorf("Greeting(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
