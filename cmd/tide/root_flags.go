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
	"fmt"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tidev1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

// configFlags carries the kubectl-aligned kubeconfig resolution flags
// (--kubeconfig, --context, --namespace, --cluster, --user, --token, etc.) per
// D-C1. The full set ships via genericclioptions.NewConfigFlags so `tide`
// behaves identically to `kubectl` for cluster targeting.
//
// configFlags is package-level because every subcommand calls RESTConfig() or
// K8sClient(); per-root-tree instances would not share resolution state.
var configFlags = genericclioptions.NewConfigFlags(true)

// outputFormat is the value of --output|-o. Subcommands that render structured
// output (inspect-wave, describe-budget) consult outputFormat in their RunE.
//
// D-C4 limits the format set to {human, json}. YAML is intentionally omitted —
// kubectl already does YAML output of K8s objects.
var outputFormat = "human"

// scheme is the shared client.Object scheme registered with both client-go
// builtins and api/v1alpha3 (Project, Milestone, Phase, Plan, Task, Wave).
// Lazily constructed on first K8sClient() call to avoid init-order coupling.
var scheme = newScheme()

func newScheme() *runtime.Scheme {
	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	utilruntime.Must(tidev1alpha3.AddToScheme(s))
	return s
}

// registerPersistentFlags wires configFlags + --output|-o into the root
// command's PersistentFlags so every subcommand inherits them.
func registerPersistentFlags(root *cobra.Command) {
	configFlags.AddFlags(root.PersistentFlags())
	root.PersistentFlags().StringVarP(
		&outputFormat,
		"output", "o",
		"human",
		"Output format: human, json",
	)
}

// RESTConfig builds a *rest.Config from the resolved kubeconfig chain.
// Subcommands call this in their RunE; the constructor is lazy so a missing
// kubeconfig only fails when an actual API call is attempted.
func RESTConfig() (*rest.Config, error) {
	cfg, err := configFlags.ToRESTConfig()
	if err != nil {
		return nil, fmt.Errorf("resolve kubeconfig: %w", err)
	}
	return cfg, nil
}

// K8sClient builds a controller-runtime client.Client with the TIDE scheme
// registered. Subcommands prefer this over raw clientsets so List/Get/Patch
// returns strongly-typed api/v1alpha3 objects.
func K8sClient() (client.Client, error) {
	cfg, err := RESTConfig()
	if err != nil {
		return nil, err
	}
	c, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("build k8s client: %w", err)
	}
	return c, nil
}

// resolveNamespace returns the operator-targeted namespace using the
// genericclioptions resolution chain (--namespace flag -> kubeconfig context
// -> "default"). Subcommands that need a namespace call this to honour the
// kubectl-aligned UX surface.
func resolveNamespace() (string, error) {
	ns, _, err := configFlags.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return "", fmt.Errorf("resolve namespace: %w", err)
	}
	return ns, nil
}
