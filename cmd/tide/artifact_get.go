/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"github.com/spf13/cobra"
)

// newArtifactGetCmd is registered in Task 1; Task 2 fills in the inspector-pod
// proxy implementation.
func newArtifactGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "artifact-get <namespace>/<project>/<path>",
		Short: "Fetch a PVC artifact via an apiserver-proxied inspector pod",
		Long: "tide artifact-get streams a file from the per-Project PVC to stdout via a short-lived " +
			"busybox inspector pod (pods/exec). Ref form: <namespace>/<project>/<relative-path>.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runArtifactGet(cmd, args)
		},
	}
}
