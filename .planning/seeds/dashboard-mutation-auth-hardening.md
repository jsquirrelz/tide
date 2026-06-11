---
title: Dashboard mutation auth hardening
trigger_condition: Dashboard exposed beyond port-forward/trusted perimeter, OR before promoting the Project editor in public-facing docs
planted_date: 2026-06-10
---

# Dashboard mutation auth hardening

The Project editor (dogfood project 3) ships with **perimeter-trust auth**: no app-level
gate on the create/update endpoints, documented requirement that the dashboard sits
behind port-forward or ingress-level auth. Acceptable for our own cluster; not
acceptable as a durable answer for an open-source chart installed in arbitrary clusters
— the endpoints create workload-spawning CRs.

Hardening ladder when the trigger fires:

1. **Chart-provisioned bearer token** on mutation endpoints only (reads + SSE stay
   open). Small, provider-agnostic, no identity dependency.
2. **K8s-native SubjectAccessReview**: forward the caller's K8s credentials and SSAR
   create/update on Projects. Most correct; needs an OIDC/kubeconfig story for the
   browser context.

Keep the gate config in the chart, not baked into the controller (same principle as
human-gate policy).
