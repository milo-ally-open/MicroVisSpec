---
name: project-spec-workflow
description: Create, review, and maintain Yunshao's grouped product and engineering specifications before and during implementation. Use for feature, defect, cross-service, device, and prototype work; use the light path for small reversible changes.
---

# Yunshao Spec Workflow

Use this skill when a request needs explicit scope, behavior, design choices, implementation tasks, or acceptance evidence. The project’s persistent spec groups live under `project/docs/Specification/<domain>/`; Git branches, commits, and PRs provide change history.

## Read the project context

1. Find the repository root and read applicable `AGENTS.md` files.
2. Read `project/docs/WORKFLOW.md`, `project/docs/RULES.md`, `project/docs/Specification/README.md`, and `project/docs/Specification/WORKFLOW.md`.
3. Inspect `Analysis/PRD.md`, relevant `Research/RESEARCH.md`, `Design/STRUCTURE.md`, `Analysis/SPEC.md`, the matching domain group, and the actual implementation. Treat plans as intended behavior until verified in code.
4. Check branch, status, and diff before editing. Preserve unrelated changes and never overwrite user work.

## Pick the smallest useful path

- **Light:** one low-risk, local, reversible adjustment. Record purpose, scope, unchanged behavior, and observable acceptance in the matching group’s `proposal.md`; do not manufacture a full document set.
- **Standard:** a feature or defect with user-visible behavior. Maintain the matching group’s `proposal.md`, `spec.md`, `design.md` when there are meaningful technical choices, and `tasks.md`.
- **High risk / cross-boundary:** include every affected domain, explicit requirement-to-module/task/acceptance traceability, failure and rollback behavior, and relevant security, compatibility, data, operations, and hardware constraints.
- **Prototype:** state the unknown, evidence sought, success/failure and stop criteria, and disposable boundary. Record findings; convert validated conclusions into the durable spec before treating the prototype as product behavior.

If no suitable domain group exists, use `scripts/init_spec_group.py` to create one from the project templates. It refuses to overwrite existing files. For an existing group, edit the authoritative files in place. Do not create one folder per task or duplicate the existing baseline just to record a code change.

## Draft and reconcile

1. Summarize the caller, problem, observable outcome, scope, and non-goals. Separate evidence, user decisions, recommendations, `[待确认]` product choices, and `[假设]` technical judgments. Ask only about decisions that block safe scope, authorization, compatibility, data, or hardware commitments; keep non-blocking uncertainty explicit with an owner or validation path.
2. Write behavior as stable `REQ-xx` requirements with testable `AC-xx.n` acceptance conditions. Cover normal paths and relevant errors, boundaries, permissions, and compatibility. Link each requirement to the upstream PRD/decision and the responsible module.
3. Design only the decisions needed to build and review the change: boundaries, APIs/data, state/error handling, security, operations, compatibility, and rollback. Explain material alternatives; state when a separate design is unnecessary.
4. Make each `T-xx` task independently checkable and map it to requirements and acceptance IDs. Keep tasks in `tasks.md`; use Git history for implementation history and review.
5. Before coding, compare proposal, spec, design, tasks, project baselines, and code. Resolve contradictions and blocking unknowns. The included validator checks document presence, unresolved template markers, ID uniqueness, and REQ/AC/task links; it does not replace engineering review.
6. Implement in task-sized steps. Update task checkboxes and the owning spec when evidence changes a decision. Keep durable truth in one place: product scope in PRD, external evidence in Research, system boundaries in Design, cross-service contracts in the global SPEC, and local contracts in the corresponding domain group.
7. Verify the applicable acceptance conditions and record only checks actually run, with outcomes and evidence. For device/model work, name the hardware and versions and distinguish simulation from real-device results. Reconcile the specification and implementation in the same Git review; do not claim completion when acceptance evidence is missing.

## Scripts

- Scaffold a domain group: `python codearts_proof/skills/project-spec-workflow/scripts/init_spec_group.py --root . --name <domain> --title "<title>" --mode standard`
- Check the group: `python codearts_proof/skills/project-spec-workflow/scripts/validate_spec_group.py project/docs/Specification/<domain>`
- Read `project/docs/Specification/WORKFLOW.md` and the templates for the detailed project-specific conventions. Script success is structural validation, not approval or proof that the implementation works.

## Report

Give the document group path and status, affected implementation scope, acceptance evidence, checks not performed, baseline files synchronized, and unresolved risks. Do not imply user approval, deployment, or hardware validation without evidence.
