/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	"os"
	"path/filepath"
)

// readSourceFile reads a file in the same internal/controller/ directory as the
// test binary. Used by Plan 04-05 Task 2 to assert grep contracts at runtime
// (mirrors the verification block's `grep -c AnnotationChangedPredicate ...`).
func readSourceFile(relName string) (string, error) {
	// Tests run with cwd = the package directory (internal/controller).
	b, err := os.ReadFile(filepath.Clean(relName))
	if err != nil {
		return "", err
	}
	return string(b), nil
}
