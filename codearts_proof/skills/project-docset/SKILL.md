---
name: project-docset
description: Create or maintain a project-agnostic software-engineering document set through interactive PRD discovery, evidence-based research, system design, and traceable technical specification. Use when a user asks to reproduce, distill, or evolve a complete project documentation workflow.
---

# Project Docset

This skill guides an AI Agent through a staged, human-in-the-loop software-engineering documentation process. Treat the documents as one connected decision record, not independent prose files. The user owns product decisions; the agent makes uncertainty visible, offers concrete choices, records the answers, and maintains traceability across stages.

For a full document-set reproduction, read [the document map and source boundaries](references/docset-map.md) before creating files. Start from the user's actual current working directory, inspect the repository and target project's directory structure, and follow its existing documentation root. A project/ directory is not required. The logical artifacts below are portable; project/docs is only the path used by the source example.

## Discovering the active project and documentation root

Determine whether the current working directory is the repository root, a product subtree, or one project inside a monorepo. Inspect the actual tree, README locations, existing documentation, and Git root before choosing a destination. Reuse the documentation root and naming convention already associated with the user's target. If none exists, use docs/ under the active project root. Use project/docs only when the repository actually has a relevant project/ subtree or the user explicitly asks to reproduce that exact layout. Never create a project/ directory merely to imitate the source example. If the current directory contains multiple plausible projects and context does not identify the target, ask a focused question before creating a second document set.

## Shared operating model

- Start from repository evidence: README, existing docs, code, prototypes, hardware/deployment constraints, and user answers. Inspect before asking. Do not ask the user to repeat information already present.
- Keep product scope, engineering decisions, assumptions, and open questions distinct. Never turn a guess into a requirement. Label uncertain claims [假设] or [待确认].
- Keep terms stable across artifacts. If sources conflict, show the conflict and ask the decision owner to resolve it; do not silently choose a side.
- Use an interactive question tool when the host provides one. Otherwise ask directly in chat and wait for answers. A question list written only into a document or status message does not count as asking.
- Ask no more than three questions in one round. Provide candidate answers and “其他”; mark each [必答] or [可选]. Order questions by how much they block progress. After every answered round, update the relevant document immediately and report a short “本轮变更摘要” before continuing.
- Use relative links for document assets. Store diagrams beside the document that references them, under that document area's assets/ directory. Never leave a broken asset link.
- Keep a change log in each authored decision document. Mark accepted decisions, assumptions, and remaining questions so a new agent can resume without reconstructing the conversation.

## Temporary branch lifecycle

The source workflow calls for a temporary branch for PRD work and another for SPEC work, on both local and remote, then deletion after the relevant document is confirmed stable. Preserve that lifecycle when operating in a repository:

1. At the start of a PRD or SPEC workstream, create or reuse a clearly named temporary stage branch locally and remotely, subject to the current host's authorization, repository policy, and available Git access. Do not claim a remote branch exists unless verified.
2. Keep the branch until the user confirms that PRD or SPEC is stable. “Draft complete; please review” is not that confirmation.
3. Before deleting either branch, ensure accepted work is preserved in the user's chosen base or another durable location and deletion will not discard unmerged work. Then remove the local and remote temporary branches as authorized.
4. If remote mutation is not authorized or unavailable, do not bypass that boundary. Explain which remote step could not be completed; proceed locally only if allowed, and report the branch state accurately.

Do not create nested temporary branches when continuing the same active workstream. If an existing branch contains unrelated or uncommitted work, preserve it and choose a safe path consistent with repository policy.

## PRD workstream: product discovery

Adopt the stance of a senior product manager / Forward Deployed Engineer. The user is the customer and decides product scope.

**Input and start**

- Use the repository README as the primary project brief. If it is missing, ask the user for it before drafting requirements.
- Read relevant project docs and prototypes as supporting context. Competitor information may inform research, but does not itself authorize adding features.
- First give a concise restatement of your understanding (at most 300 Chinese characters, or an equivalent limit in the user's language), then list 3–5 important unknowns. Ask only about information not already answered by the sources.
- If the README, existing docs, or user's answers contradict one another, surface the exact conflict and ask the user to arbitrate.

**Interactive discovery**

For each round, ask at most three real questions through the host's question UI or chat. Mark questions [必答] or [可选], offer useful choices plus “其他”, and make the consequences of choices clear. Ask for decisions about target users, core situation, MVP boundary, measurable success, constraints, risks, and exclusions as needed. Do not front-load every possible question; ask the next most useful set based on what remains unknown.

After each user answer, immediately update the PRD and its change log, then report “本轮变更摘要”. Keep earlier accepted answers intact. Do not wait until the end of the interview to write the document.

**PRD contract**

Default output: <selected-docs-root>/Analysis/PRD.md. In the source example this was project/docs/Analysis/PRD.md; choose the docs root by inspecting the user's current project structure, not by assuming that path exists.

Include a rigorous user-story section. Use the pattern “I am [role], I want [capability/action], so that [observable value]” or its equivalent in the user's language, for example: “作为[角色]，我希望[能力/动作]，以便[可观察的价值]。” Stories must identify a user/role, desired capability, and reason/value; avoid vague feature labels.

Cover these topics:

- 背景问题
- 用户画像
- 场景
- 用户故事
- 功能范围：MVP / 迭代 / 不做
- 成功指标
- 约束与假设
- 风险与依赖
- 待确认
- 变更记录

Do not write a technical solution in the PRD. Do not invent demand, metrics, user roles, or facts. Record unknowns as [假设] or [待确认], with an owner or resolution path when known. Keep non-goals explicit.

**PRD review gate**

A PRD is ready for first review only when target users, core situation, MVP scope, success measures, and non-goals are clear, and no more than three material items remain open. If the criteria are met, state exactly: **PRD初稿完成，请审阅**. Stop the discovery loop at that review gate; do not treat the draft as user-approved or stable until the user says so.

## Research and design workstreams

In the source pattern, the workflow order is Analysis → Research → Design → Specification. Research and Design files exist, but their content and detailed course rules were not provided. When a full set is requested, use the evidence-based, minimal defaults in the document-map reference and label them as portable recommendations rather than source-verified course rules.

Research should answer a defined question with traceable sources and dates, distinguish evidence from interpretation, compare relevant alternatives, and state implications for the PRD/design plus remaining uncertainty. It may refine understanding, but must not silently expand product scope.

Design should explain the system boundary, major elements and responsibilities, and important interactions or flows at the level needed to communicate structure. Link each design choice to a PRD need or research finding. Keep detailed API, data, and operational contracts in the technical SPEC or a separately requested specification.

Do not claim to have performed external research unless sources were actually consulted. Ask only for user decisions that cannot be resolved from available evidence and matter to the result.

## SPEC workstream: technical decisions and traceability

Adopt the stance of a senior software/hardware architect or technical lead. The user is both the customer and PRD owner. Do not re-open settled product decisions without pointing out a concrete conflict or technical blocker.

**Input and start**

- Use the PRD under the selected documentation root as the primary input. If it is missing, ask the user for it. If it conflicts with the README or other authoritative project material, quote or summarize the conflicting statements and ask the user before making a consequential technical commitment.
- Inspect the codebase and applicable hardware, deployment, compatibility, cost, performance, and compliance constraints.
- First show a PRD→SPEC mapping table and 3–5 key technical unknowns. Map each requirement or story to a proposed module, interface, data element, quality requirement, or an explicit “本版本不实现” disposition. Do not leave any PRD requirement unmapped.

**Interactive decision rounds**

Use the available question UI or chat, not a passive list in a document. Ask no more than three questions per round, ordered by blocking impact. Prioritize performance, compatibility, deployment, cost, and compliance when relevant. Present candidate options, tradeoffs, and “其他”; mark [必答] or [可选]. Avoid questions whose answers are already in the PRD, README, code, or constraints.

After every answer round, immediately update the SPEC and its change log, then report “本轮变更摘要”. Keep user decisions distinct from agent recommendations. When a decision remains open, state its effect and mark [假设] or [待确认].

**Technical SPEC contract**

Default output: <selected-docs-root>/Analysis/SPEC.md. In the source example this was project/docs/Analysis/SPEC.md; do not create a project/ subtree just to use that path.

Cover these topics:

- 概述与 PRD 对应关系
- 架构与系统边界
- 模块职责
- 接口契约
- 数据模型
- 非功能指标
- 依赖
- 部署与运维
- 风险与被否决方案
- 待确认
- 变更记录

For every PRD requirement, identify its technical landing or explicitly state that it is outside this version. Do not add business requirements beyond the PRD. For every material technology or architecture selection, state the reason, at least one credible rejected alternative, and why it was rejected. Distinguish decisions for “本版本” from “未来演进”. Keep technical claims tied to repository evidence, constraints, or user-confirmed decisions.

When diagrams are useful, save architecture, sequence/flow, and ER diagrams in <selected-docs-root>/Analysis/assets/ and reference them with relative paths from SPEC.md. Do not create decorative diagrams that add no information.

**SPEC review gate**

The system boundary, modules, interfaces, data model, and non-functional measures must be explicit. Every PRD requirement must have a technical disposition. No more than three material questions may remain open. When these conditions are met, state exactly: **SPEC初稿完成，请审阅**. Stop at the review gate. Do not call the SPEC stable, delete its temporary branch, or imply approval until the user confirms.

## Full-set completion and final pass

For the portable baseline, map logical outputs to the documentation root selected from the active project structure. When no documentation convention exists, use docs/ beneath the active project root. The source example used project/docs because its repository had a project/ subtree; that is not a required layout. Its logical structure, relative to <docs-root>, is:

- WORKFLOW.md
- RULES.md
- Analysis/PRD.md and Analysis/SPEC.md
- Research/RESEARCH.md
- Design/STRUCTURE.md
- Analysis/assets/, Research/assets/, Design/assets/, and Specification/assets/

Use <docs-root>/Specification/assets/ for separately requested detailed specification artifacts. Do not invent standalone Specification documents because the source example contains no defined document there. Read the reference for exact evidence boundaries and suggested lightweight skeletons.

Before calling the set complete, verify that:

- the stage order, document links, terminology, scope, and decisions agree;
- each PRD requirement is traceable into research/design/specification or explicitly deferred;
- assumptions, unresolved decisions, risks, rejected alternatives, and change history are visible;
- asset links resolve relative to their Markdown files;
- RULES.md contains only user-provided or user-approved project rules, not policies inferred from filenames or examples;
- review and stability states are reported accurately.

Do not call generated files “course standard” when their structure was not present in the source materials.
