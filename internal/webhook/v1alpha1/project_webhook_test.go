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

package v1alpha1

import (
	"context"
	"strings"
	"testing"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

// TestProjectWebhook is the top-level test group for ProjectCustomValidator.

func TestProjectWebhook_RejectsAdminPath(t *testing.T) {
	v := &ProjectCustomValidator{}
	p := &tideprojectv1alpha1.Project{
		Spec: tideprojectv1alpha1.ProjectSpec{
			TargetRepo: "https://github.com/example/repo",
			Providers: []tideprojectv1alpha1.ProviderConfig{
				{Name: "anthropic", AllowedRoutes: []tideprojectv1alpha1.RouteSpec{
					{Method: "POST", PathPrefix: "/v1/admin/users"},
				}},
			},
		},
	}
	_, err := v.ValidateCreate(context.Background(), p)
	if err == nil || !strings.Contains(err.Error(), "denylist") {
		t.Errorf("expected denylist rejection; got %v", err)
	}
}

func TestProjectWebhook_RejectsBillingPath(t *testing.T) {
	v := &ProjectCustomValidator{}
	p := &tideprojectv1alpha1.Project{
		Spec: tideprojectv1alpha1.ProjectSpec{
			TargetRepo: "https://github.com/example/repo",
			Providers: []tideprojectv1alpha1.ProviderConfig{
				{Name: "anthropic", AllowedRoutes: []tideprojectv1alpha1.RouteSpec{
					{Method: "GET", PathPrefix: "/v1/billing"},
				}},
			},
		},
	}
	_, err := v.ValidateCreate(context.Background(), p)
	if err == nil {
		t.Errorf("expected denylist rejection for /v1/billing")
	}
}

func TestProjectWebhook_RejectsBillingSubpath(t *testing.T) {
	v := &ProjectCustomValidator{}
	p := &tideprojectv1alpha1.Project{
		Spec: tideprojectv1alpha1.ProjectSpec{
			TargetRepo: "https://github.com/example/repo",
			Providers: []tideprojectv1alpha1.ProviderConfig{
				{Name: "anthropic", AllowedRoutes: []tideprojectv1alpha1.RouteSpec{
					{Method: "POST", PathPrefix: "/v1/billing/invoices"},
				}},
			},
		},
	}
	_, err := v.ValidateCreate(context.Background(), p)
	if err == nil {
		t.Errorf("expected denylist rejection for /v1/billing/invoices")
	}
}

func TestProjectWebhook_AcceptsValidRoute(t *testing.T) {
	v := &ProjectCustomValidator{}
	p := &tideprojectv1alpha1.Project{
		Spec: tideprojectv1alpha1.ProjectSpec{
			TargetRepo: "https://github.com/example/repo",
			Providers: []tideprojectv1alpha1.ProviderConfig{
				{Name: "anthropic", AllowedRoutes: []tideprojectv1alpha1.RouteSpec{
					{Method: "POST", PathPrefix: "/v1/files"},
				}},
			},
		},
	}
	_, err := v.ValidateCreate(context.Background(), p)
	if err != nil {
		t.Errorf("expected acceptance for /v1/files; got %v", err)
	}
}

func TestProjectWebhook_AcceptsEmptyAllowedRoutes(t *testing.T) {
	v := &ProjectCustomValidator{}
	p := &tideprojectv1alpha1.Project{
		Spec: tideprojectv1alpha1.ProjectSpec{
			TargetRepo: "https://github.com/example/repo",
		},
	}
	_, err := v.ValidateCreate(context.Background(), p)
	if err != nil {
		t.Errorf("expected no error on empty Providers; got %v", err)
	}
}

func TestProjectWebhook_ValidateUpdate_RejectsAdminPath(t *testing.T) {
	v := &ProjectCustomValidator{}
	old := &tideprojectv1alpha1.Project{
		Spec: tideprojectv1alpha1.ProjectSpec{
			TargetRepo: "https://github.com/example/repo",
		},
	}
	newObj := &tideprojectv1alpha1.Project{
		Spec: tideprojectv1alpha1.ProjectSpec{
			TargetRepo: "https://github.com/example/repo",
			Providers: []tideprojectv1alpha1.ProviderConfig{
				{Name: "anthropic", AllowedRoutes: []tideprojectv1alpha1.RouteSpec{
					{Method: "POST", PathPrefix: "/v1/admin"},
				}},
			},
		},
	}
	_, err := v.ValidateUpdate(context.Background(), old, newObj)
	if err == nil || !strings.Contains(err.Error(), "denylist") {
		t.Errorf("expected denylist rejection on update; got %v", err)
	}
}

func TestProjectWebhook_ValidateDelete_IsNoop(t *testing.T) {
	v := &ProjectCustomValidator{}
	p := &tideprojectv1alpha1.Project{}
	_, err := v.ValidateDelete(context.Background(), p)
	if err != nil {
		t.Errorf("expected no error on delete; got %v", err)
	}
}
