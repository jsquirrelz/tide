---
created: 2026-07-03T18:08:15.231Z
title: Update subagent pricing table for Claude 5 family models
area: subagent
files:
  - cmd/claude-subagent
---

## Problem

The claude-subagent's cost-tally pricing table predates the Claude 5 family.
Observed on the first external-repo run (2026-07-03), subagent log:

    pricing: unknown model "claude-sonnet-5", using conservative default (most-expensive known tier)

Every dispatch on `claude-sonnet-5`, `claude-fable-5`, or `claude-opus-4-8`
is billed into `BudgetStatus.CostSpentCents` at the most-expensive known
tier. The conservative fallback is the right fail-safe, but with current
models it overcounts spend several-fold and trips `absoluteCapCents`
(Phase=BudgetExceeded) far earlier than real spend justifies.

Ground truth from the completed run: TIDE's final tally was $10.86, while
the Anthropic developer console reported $3.84 actual spend for the API key
— a 2.8x overcount. (Model mix: 1 Sonnet-5 + 1 Fable-5 + 2 Sonnet-5 planner
dispatches, 3 Haiku-4.5 task executions.)

## Solution

Add pricing rows for the current lineup (per MTok in/out): claude-fable-5
$10/$50, claude-opus-4-8 $5/$25, claude-sonnet-5 $3/$15, plus keep existing
4.x rows. Grep for where the existing table lives (the "pricing: unknown
model" log line is the anchor). Consider a longer-term follow-up: make the
table chart-configurable so new models don't require an image rebuild.
