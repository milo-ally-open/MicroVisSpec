---
name: project-docset-initialization
description: Create or maintain a project-agnostic software-engineering document set through interactive PRD discovery, evidence-based research, system design, and traceable technical specification.
---

# Project Docset

This skill guides an AI agent through a staged, human-in-the-loop software-engineering documentation process. Treat the documents as one connected decision record, not as independent prose files. The user owns product decisions; the agent makes uncertainty visible, offers concrete choices, records answers, and maintains traceability across stages.

For a full document-set reproduction, read [the document map and source boundaries](references/docset-map.md) before creating files. Start from the user's actual working directory, inspect the repository and target project's structure, and follow its existing documentation root. A `project/` directory is not required. The artifacts below are portable; `project/docs/` is only the path used by the source example.

## Discover the active project and documentation root

Determine whether the current working directory is the repository root, a product subtree, or one project in a monorepo. Inspect the actual tree, README locations, existing documentation, and Git root before choosing a destination. Reuse the documentation root and naming convention already associated with the target. If none exists, use `docs/` under the active project root. Use `project/docs/` only when the repository has a relevant `project/` subtree or the user explicitly requests that layout. Never create `project/` merely to imitate the source example. If several projects are plausible and context does not identify the target, ask a focused question before creating another document set.

## Shared operating model

- Start from repository evidence: README, existing documentation, code, prototypes, hardware and deployment constraints, and user answers. Inspect before asking. Do not ask the user to repeat information already available.
- Keep product scope, engineering decisions, assumptions, and open questions distinct. Never present a guess as a requirement. Mark uncertainty as `[Assumption]` or `[To confirm]`.
- Keep terminology stable across artifacts. If sources conflict, show the conflict and ask the decision owner to resolve it; do not silently pick a side.
- Use an interactive question tool when the host provides one. Otherwise ask directly in chat and wait for answers. A question list written only in a document or status update does not count as asking.
- Ask no more than three questions per round. Offer candidate answers and an “Other” option; label each question `[Required]` or `[Optional]`. Order questions by how much they block progress. After each answered round, immediately update the relevant document and its change log, then provide a short “Round update” before continuing.
- Use relative links for document assets. Store diagrams beside the document that references them, under that document area's `assets/` directory. Do not leave broken asset links.
- Keep a change log in each authored decision document. Mark accepted decisions, assumptions, and remaining questions so another agent can resume without reconstructing the conversation.

## Temporary branch lifecycle

The source workflow calls for one temporary branch for PRD work and another for SPEC work, locally and remotely, then deletion after the relevant document is confirmed stable. Preserve that lifecycle when operating in a repository:

1. At the start of a PRD or SPEC workstream, create or reuse a clearly named temporary stage branch locally and remotely, subject to the active host's authorization, repository policy, and available Git access. Do not claim that a remote branch exists unless verified.
2. Keep the branch until the user confirms that the PRD or SPEC is stable. “Draft complete; please review” is not confirmation of stability.
3. Before deleting either branch, ensure accepted work is preserved in the user's chosen base or another durable location and that deletion will not discard unmerged work. Then remove local and remote temporary branches as authorized.
4. If remote mutation is not authorized or available, do not bypass that boundary. Explain which remote step could not be completed, proceed locally only if allowed, and report branch state accurately.

Do not create nested temporary branches when continuing the same active workstream. If an existing branch contains unrelated or uncommitted work, preserve it and choose a safe path consistent with repository policy.

## PRD workstream: product discovery

Adopt the stance of a senior product manager or Forward Deployed Engineer. The user is the customer and decides product scope.

### Input and start

- Use the repository README as the primary project brief. If it is missing, ask the user for it before drafting requirements.
- Read relevant project documentation and prototypes as supporting context. Competitor information may inform research, but does not authorize adding features by itself.
- First give a concise restatement of your understanding (at most 300 characters, or an equivalent limit in the user's language), then list three to five important unknowns. Ask only about information not already answered by sources.
- If the README, existing documentation, or user's answers conflict, surface the exact conflict and ask the user to decide.

### Interactive discovery

Ask at most three real questions per round through the host's question UI or chat. Label questions `[Required]` or `[Optional]`, offer useful choices plus “Other,” and explain the consequence of each choice. Ask about target users, core situation, MVP boundary, measurable success, constraints, risks, and exclusions as needed. Do not front-load every possible question; choose the next most useful questions based on what remains unknown.

After each answer, immediately update the PRD and its change log, then provide a “Round update.” Preserve earlier accepted answers. Do not wait until the interview ends to write the document.

### PRD contract

Default output: `<selected-docs-root>/Analysis/PRD.md`. The source example used `project/docs/Analysis/PRD.md`; select the root by inspecting the active project rather than assuming that path exists.

Include rigorous user stories. Use “As a [role], I want [capability or action], so that [observable value].” Stories must identify a user or role, the desired capability, and its value; avoid vague feature labels.

Cover:

- Problem background
- User profiles
- Usage scenarios
- User stories
- Scope: MVP, later iterations, and explicit non-goals
- Success measures
- Constraints and assumptions
- Risks and dependencies
- Open questions
- Change log

Do not put a technical solution in the PRD. Do not invent demand, metrics, user roles, or facts. Record unknowns as `[Assumption]` or `[To confirm]`, with an owner or resolution path when known. Keep non-goals explicit.

### PRD review gate

A PRD is ready for initial review only when target users, the core situation, MVP scope, success measures, and non-goals are clear, and no more than three material items remain open. When these conditions are met, state exactly: **PRD draft complete — ready for review.** Stop the discovery loop at this review gate. Do not treat the draft as user-approved or stable until the user says so.

## Research and design workstreams

The source pattern orders work as Analysis → Research → Design → Specification. Research and Design files exist, but their detailed structure and process rules were not provided. For a full document set, use the evidence-based, minimal defaults in the document-map reference and label them as portable recommendations, not source-verified course rules.

Research should answer a defined question with traceable sources and dates, distinguish evidence from interpretation, compare relevant alternatives, and state implications for the PRD or design plus remaining uncertainty. Research may refine understanding, but must not silently expand product scope.

Design should explain the system boundary, major elements and responsibilities, and important interactions or flows at the level needed to communicate structure. Link each design choice to a PRD need or research finding. Keep detailed API, data, and operational contracts in the technical SPEC or a separately requested specification.

Do not claim to have performed external research unless sources were actually consulted. Ask only for user decisions that cannot be resolved from available evidence and matter to the result.

## SPEC workstream: technical decisions and traceability

Adopt the stance of a senior software or hardware architect or technical lead. The user is both the customer and PRD owner. Do not reopen settled product decisions without identifying a concrete conflict or technical blocker.

### Input and start

- Use the PRD under the selected documentation root as the primary input. If it is missing, ask the user for it. If it conflicts with the README or other authoritative project material, quote or summarize the conflict and ask before making a consequential technical commitment.
- Inspect the codebase and applicable hardware, deployment, compatibility, cost, performance, and compliance constraints.
- First show a PRD-to-SPEC mapping table and three to five key technical unknowns. Map every requirement or story to a proposed module, interface, data element, quality requirement, or an explicit “Not implemented in this version” disposition. Do not leave any PRD requirement unmapped.

### Interactive decision rounds

Use the available question UI or chat rather than a passive list in a document. Ask no more than three questions per round, ordered by blocking impact. Prioritize performance, compatibility, deployment, cost, and compliance when relevant. Present candidate options, trade-offs, and “Other”; label each question `[Required]` or `[Optional]`. Avoid questions already answered by the PRD, README, code, or constraints.

After each answer round, immediately update the SPEC and its change log, then provide a “Round update.” Keep user decisions distinct from agent recommendations. When a decision remains open, state its effect and mark it `[Assumption]` or `[To confirm]`.

### Technical SPEC contract

Default output: `<selected-docs-root>/Analysis/SPEC.md`. The source example used `project/docs/Analysis/SPEC.md`; do not create a `project/` subtree merely to use that path.

Cover:

- Overview and PRD traceability
- Architecture and system boundary
- Module responsibilities
- Interface contracts
- Data model
- Non-functional measures
- Dependencies
- Deployment and operations
- Risks and rejected alternatives
- Open questions
- Change log

For every PRD requirement, identify its technical landing or explicitly state that it is outside this version. Do not add business requirements beyond the PRD. For each material technology or architecture choice, state the reason, at least one credible rejected alternative, and why it was rejected. Distinguish decisions for “this version” from “future evolution.” Tie technical claims to repository evidence, constraints, or user-confirmed decisions.

When diagrams help, save architecture, sequence or flow, and ER diagrams in `<selected-docs-root>/Analysis/assets/` and reference them with relative paths from `SPEC.md`. Do not create decorative diagrams that add no information.

### SPEC review gate

The system boundary, modules, interfaces, data model, and non-functional measures must be explicit. Every PRD requirement must have a technical disposition. No more than three material questions may remain open. When these conditions are met, state exactly: **SPEC draft complete — ready for review.** Stop at the review gate. Do not call the SPEC stable, delete its temporary branch, or imply approval until the user confirms.

## Full-set completion and final pass

Map logical outputs to the documentation root selected from the active project structure. If no documentation convention exists, use `docs/` under the active project root. The source example used `project/docs/` because its repository had a `project/` subtree; that is not a required layout. Its logical structure, relative to `<docs-root>`, is:

- `WORKFLOW.md`
- `RULES.md`
- `Analysis/PRD.md` and `Analysis/SPEC.md`
- `Research/RESEARCH.md`
- `Design/STRUCTURE.md`
- `Analysis/assets/`, `Research/assets/`, `Design/assets/`, and `Specification/assets/`

Use `<docs-root>/Specification/assets/` for separately requested detailed specification artifacts. Do not invent standalone Specification documents because the source example defines no document there. Read the reference for evidence boundaries and suggested lightweight outlines.

Before calling the set complete, verify that:

- Stage order, document links, terminology, scope, and decisions agree.
- Each PRD requirement is traceable into research, design, or specification, or is explicitly deferred.
- Assumptions, unresolved decisions, risks, rejected alternatives, and change history are visible.
- Asset links resolve relative to their Markdown files.
- `RULES.md` contains only user-provided or user-approved project rules, not policies inferred from filenames or examples.
- Review and stability states are reported accurately.

Do not call generated files “course standard” when their structure was not present in the source materials.
