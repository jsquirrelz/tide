// SPDX-License-Identifier: MIT
// Copyright (c) 2026 The TIDE Authors

package main

import "fmt"

// Greeting returns a deterministic greeting for name. The TIDE medium-sample
// outcome prompt operates on this function — TIDE plans a modification,
// commits the change to the local-only git remote, and pushes back.
func Greeting(name string) string {
	return "Hello, " + name + "!"
}

func main() {
	fmt.Println(Greeting("world"))
}
