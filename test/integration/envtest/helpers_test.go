/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package envtest_integration

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
)

// ensureLiveProject creates the given Project, tolerating the previous spec's
// asynchronous deletion of a same-named object.
//
// A previous spec's AfterEach deletes its Project asynchronously (finalizer
// removal happens on a later reconcile). A bare Create + IgnoreAlreadyExists
// can land on the still-terminating object, get silently swallowed, and leave
// the spec with NO Project once that deletion completes — reconcilers then
// drop every mapped reconcile at the NotFound fetch and derived resources
// (e.g. Wave CRs) never appear (the global_wave_derivation CI flake, PR #9).
// Retry until a live (non-terminating) Project exists.
//
// The prototype is deep-copied per attempt so a partially-mutated object from
// a failed Create (e.g. a stamped resourceVersion) never poisons the retry.
func ensureLiveProject(ctx context.Context, proto *tideprojectv1alpha2.Project) {
	GinkgoHelper()
	Eventually(func() error {
		err := k8sClient.Create(ctx, proto.DeepCopy())
		if apierrors.IsAlreadyExists(err) {
			var existing tideprojectv1alpha2.Project
			if getErr := k8sClient.Get(ctx, client.ObjectKeyFromObject(proto), &existing); getErr != nil {
				// NotFound: deletion completed between Create and Get — the
				// next attempt recreates it.
				return getErr
			}
			if !existing.DeletionTimestamp.IsZero() {
				return fmt.Errorf("project %s/%s still terminating; waiting to recreate", proto.Namespace, proto.Name)
			}
			return nil // live Project exists
		}
		return err
	}, "20s", "100ms").Should(Succeed(),
		"a live (non-terminating) Project %s/%s must exist before the spec body runs", proto.Namespace, proto.Name)
}
