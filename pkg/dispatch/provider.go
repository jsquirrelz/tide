package dispatch

// ProviderSpec selects the LLM vendor + model + per-vendor tuning knobs the
// subagent image should use to satisfy a single dispatch (D-C3). It is carried
// on every EnvelopeIn so vendor + model are orthogonal axes from the
// orchestrator's point of view: the dispatching reconciler resolves
// Project.Spec.subagent.levels.{level}.{vendor,model,params} → Project-level
// defaults → Helm-chart defaults and stamps the resolved triple here. The
// provider-side Subagent implementation (e.g. internal/subagent/anthropic)
// reads Provider.Model to drive its CLI invocation and rejects an envelope
// whose Provider.Vendor does not match the image's compiled-in sentinel —
// fail-fast defense against config drift between image tag and envelope
// content.
//
// Params is intentionally a string→string map (not a typed struct) so future
// per-vendor knobs (Anthropic thinking-budget, OpenAI temperature, Google
// safetySettings, etc.) can land without a pkg/dispatch schema bump every
// time. Validation of Params keys/values is the provider implementation's
// responsibility — pkg/dispatch only guarantees the bytes round-trip.
type ProviderSpec struct {
	// Vendor is the provider sentinel string the subagent image checks at
	// startup. Canonical values: "anthropic", "openai", "google", "xai",
	// "opencode". Required (no omitempty) — every dispatch declares a vendor.
	Vendor string `json:"vendor"`

	// Model is the per-vendor model identifier passed to the provider CLI or
	// SDK (e.g. "claude-opus-4-7", "claude-sonnet-4-6", "claude-haiku-4-5",
	// "o1-mini"). Required (no omitempty).
	Model string `json:"model"`

	// Params is the per-vendor tuning passthrough. Keys and values are
	// vendor-specific; pkg/dispatch performs no semantic validation. Omitted
	// from JSON when empty so dispatches that pass no tuning don't carry a
	// "params: {}" placeholder.
	Params map[string]string `json:"params,omitempty"`
}
