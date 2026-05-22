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

package dispatch

import "fmt"

// UnknownAPIVersionError is returned by [ValidateAPIVersionKind] when the
// envelope's apiVersion field does not match [APIVersionV1Alpha1]. Consumers
// should use errors.As to distinguish this from [*UnknownKindError].
type UnknownAPIVersionError struct {
	// APIVersion is the unrecognized value that was rejected.
	APIVersion string
}

func (e *UnknownAPIVersionError) Error() string {
	return fmt.Sprintf("envelope: unknown apiVersion %q (expected %s)", e.APIVersion, APIVersionV1Alpha1)
}

// UnknownKindError is returned by [ValidateAPIVersionKind] when the envelope's
// kind field does not match the expected kind for the call site. Consumers
// should use errors.As to distinguish this from [*UnknownAPIVersionError].
type UnknownKindError struct {
	// Kind is the unrecognized value that was rejected.
	Kind string
}

func (e *UnknownKindError) Error() string {
	return fmt.Sprintf("envelope: unknown kind %q", e.Kind)
}
