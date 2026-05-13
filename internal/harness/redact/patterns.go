package redact

import "regexp"

// maxPatternLen is an upper bound on the longest plausible secret token so
// the tail-keep buffer can hold one full pattern across a Write-call boundary.
// JWT is the most variable; 2 KiB is generous for OAuth-style JWTs and
// Anthropic API keys (Pitfall A defense — RESEARCH.md §"Pitfall A").
const maxPatternLen = 2048

// SecretPatterns is the compiled denylist applied to all RedactingWriter
// instances. Each pattern matches a class of credential string that MUST NOT
// appear in subagent stdout, stderr, or result artifacts (HARN-04 / Pitfall 18).
//
// Pattern set sources:
//   - Anthropic API key     — sk-ant-api03-<20+ alphanum>
//   - Generic sk- key       — sk-<20+ alphanum> (OpenAI-style)
//   - GitHub PAT/server     — ghp_<36 alphanum> or ghs_<36 alphanum>
//   - Slack token           — xoxa-*, xoxb-*, xoxp-* (user/bot/app tokens)
//   - AWS access key ID     — AKIA<16 upper alphanum>
//   - JWT (3-part base64)   — eyJ…<base64url>.<base64url>.<base64url>
var SecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-ant-api03-[A-Za-z0-9\-_]{20,}`),
	regexp.MustCompile(`sk-[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`gh[ps]_[A-Za-z0-9]{36}`),
	regexp.MustCompile(`xox[abp]-[A-Za-z0-9\-]+`),
	regexp.MustCompile(`AKIA[A-Z0-9]{16}`),
	regexp.MustCompile(`eyJ[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+`),
}
