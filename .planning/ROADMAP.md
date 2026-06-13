### Phase 17: Address tech debt: Plan label backfill + gate hardening

**Goal:** PlanReconciler self-heals the `tideproject.k8s/project` label like milestone/phase already do, and the reject/approve/envelope-read gate paths are brought into consistency with their shipped sibling patterns — every fix mirrors an in-tree template and carries a regression test
**Requirements**: DEBT-01, DEBT-02, DEBT-03, DEBT-04
**Depends on:** Phase 16
**Success Criteria** (what must be TRUE):

  1. A pre-v1.0.1 Plan CR with no `tideproject.k8s/project` label gets it stamped on its next reconcile (idempotently), becoming visible to `tide approve`/`tide resume` label selectors; the Project→Milestone reporter edge also stamps the label at create-site (DEBT-01)
  2. A rejected Project's completing planner Job parks the Milestone/Phase Rejected WITHOUT spawning a NEW reporter Job — and never deletes an in-flight Job (DEBT-02)
  3. `tide approve` refuses approval only when the approval target is itself Failed, not when an unrelated sibling level is Failed — honoring the strict-failure profile (DEBT-03)
  4. A transient envelope-read error in the Plan completion handler is non-fatal — it defers to children-based succession instead of wedging the Plan to terminal `Failed`, matching milestone/phase (DEBT-04)

**Plans:** 4/4 plans complete

Plans:
**Wave 1**

- [x] 17-01-PLAN.md — DEBT-01: PlanReconciler project-label backfill + Project→Milestone reporter-edge create-site stamp (+ backfill/idempotency/stamp regression specs)
- [x] 17-02-PLAN.md — DEBT-02: relocate the reject short-circuit ahead of the reporter spawn in milestone + phase completion handlers (+ no-reporter-Job-on-reject specs)
- [x] 17-03-PLAN.md — DEBT-03: narrow the D-07 approve guard to the approval target (Option A) + `--wave` semantics doc (+ approve table-test rows)

**Wave 2** *(blocked on 17-01 — shares plan_controller.go ownership)*

- [x] 17-04-PLAN.md — DEBT-04: make the Plan envelope-read error non-fatal (defer to children-based succession; mirror milestone/phase Pitfall-1) (+ non-terminal-Failed regression spec)
