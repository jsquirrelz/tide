/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package controller

import (
	corev1 "k8s.io/api/core/v1"
)

// batchv1Template returns a minimal PodTemplateSpec suitable for a planner
// Job at Phase 3 plan 03-08 scope. The production-ready spec (with PVC
// mount, credproxy sidecar, EnvelopeIn writer init container, signed-token
// minting, etc.) lands in a later plan once cmd/manager wires the planner
// dispatch end-to-end. This skeletal template lets the up-stack reconcilers
// dispatch Jobs in envtest without pulling in the full Phase 2 podjob.Build
// infrastructure — that infrastructure is task-specific (signed tokens,
// per-task EnvelopeIn, etc.) and a follow-up plan factors a planner variant.
func batchv1Template(_, image string) corev1.PodTemplateSpec {
	return corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			RestartPolicy:      corev1.RestartPolicyNever,
			ServiceAccountName: "tide-subagent",
			Containers: []corev1.Container{
				{
					Name:  "planner",
					Image: image,
				},
			},
		},
	}
}
