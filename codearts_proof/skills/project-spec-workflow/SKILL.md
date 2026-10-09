---
name: project-spec-workflow
description: Create or continue reviewable feature, bugfix, cross-service, or prototype Spec change packets for the Yunshao Edge Intelligence project, using the project's existing PRD, architecture, technical specs, and verification conventions.
---

# 云哨项目 Spec 工作流

Use this skill when a task changes product behavior, service contracts, data, device/media behavior, or needs a reviewable implementation plan. The canonical process and templates are in `project/docs/Specification/WORKFLOW.md`; follow that document if this skill and the project docs differ.

## Start from the repository

1. Locate the repository root and read any applicable `AGENTS.md` files.
2. Read `project/docs/WORKFLOW.md`, `project/docs/RULES.md`, and the workflow in `project/docs/Specification/WORKFLOW.md`.
3. Inspect `README.md`, relevant PRD sections, `Research/RESEARCH.md`, `Design/STRUCTURE.md`, `Analysis/SPEC.md`, the affected service specs, and actual code. For edge or model work, inspect the device-side implementation and evidence as well.
4. Check the current branch and working-tree diff. Preserve unrelated and uncommitted user changes. Never assume a document describes code that has not been inspected.

## Choose a path

- **Lightweight:** local, low-risk, reversible change with no API, data, authorization, product-scope, hardware-budget, or cross-service impact. Record goal, scope, unchanged behavior, and acceptance in one short proposal.
- **Standard feature:** create `proposal.md`, `spec.md`, `design.md` when technical choices matter, and `tasks.md` under `project/docs/Specification/changes/<YYYY-MM-DD>-<slug>/`.
- **Cross-boundary/high-risk:** use the standard packet plus requirement-to-component/task/verification traceability, failure and rollback behavior, and compatibility/security/operations review.
- **Bugfix:** record environment, reproduction, actual versus expected behavior, impact, and confirmed root cause versus hypotheses; specify regression acceptance.
- **Prototype/spike:** state the uncertainty, evidence sought, stopping condition, and disposable boundary. After learning, record evidence and update assumptions before formal implementation.

## Write and maintain the packet

- Give each requirement a stable ID (`REQ-01`) and each verifiable outcome an acceptance ID (`AC-01.1`). Cover the normal path and relevant failure, permission, compatibility, and data-retention cases.
- Keep product decisions in the PRD, evidence in Research, system structure in Design, cross-service contracts in `Analysis/SPEC.md`, and service-specific contracts in that service's spec. A change packet describes only the delta.
- Mark unconfirmed product choices `[待确认]` and evidence-based but unverified technical judgments `[假设]`. Do not turn either into an approved decision.
- Make each task independently checkable and map it to requirements/acceptance criteria. Include interface/data work, implementation, verification, operational concerns, and baseline-document updates where applicable.
- Before implementation, check that requirements, design, tasks, and scope agree. High-risk changes must resolve decisions that affect account isolation, service ownership, external contracts, device compatibility, data migration, or product promises before dependent code.
- Update task checkboxes as work proceeds. If implementation changes a decision, revise the owning source document and downstream artifacts before continuing.
- Record only validations actually performed. For hardware/model work, name the device, firmware/model/data version and whether evidence came from simulation or real hardware.
- Before marking complete, reconcile durable decisions into their one authoritative project document, record acceptance evidence, then move the finished or cancelled packet into `changes/archive/` with its history intact.

## Final response

Report the packet path and status, implementation scope, acceptance evidence, checks not performed, baseline documents updated, and remaining risks or open questions. Do not claim a draft is approved or hardware-verified without evidence.
