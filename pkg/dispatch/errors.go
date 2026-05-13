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
