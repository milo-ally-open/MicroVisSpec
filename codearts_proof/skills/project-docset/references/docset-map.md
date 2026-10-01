# Document map, evidence boundaries, and portable defaults

This reference separates what was explicitly present in the source workspace from what an agent may reasonably use as a portable starting point. It prevents a future agent from mistaking an empty directory or placeholder for a course rule.

## Source-backed document map

The source workspace used this layout:

- project/docs/WORKFLOW.md
- project/docs/RULES.md
- project/docs/Analysis/PRD.md
- project/docs/Analysis/SPEC.md
- project/docs/Analysis/assets/
- project/docs/Research/RESEARCH.md
- project/docs/Research/assets/
- project/docs/Design/STRUCTURE.md
- project/docs/Design/assets/
- project/docs/Specification/assets/

Its WORKFLOW.md contains the sequence **Analysis → Research → Design → Specification**. The two workflow prompts explicitly specify PRD and SPEC outputs under Analysis, so phase names and file placement do not align perfectly. Preserve the explicit output paths where that convention is being reproduced; do not silently move or duplicate the SPEC. If a different repository already has a coherent layout, preserve that layout and map the logical artifacts to it.

The prompts for PRD and SPEC are the authoritative detailed process rules in this source pattern. Their requirements are encoded in the skill entrypoint. The checked-in PRD.md, SPEC.md, RESEARCH.md, and STRUCTURE.md were empty at the time of distillation; no prior history supplied additional schemas.

## Source gaps that remain unresolved

- RESEARCH.md has a filename and directory, but no authored section structure or research method.
- Design/STRUCTURE.md has a filename and directory, but no authored section structure.
- Specification/ has an assets directory only; no standalone specification file, audience, or schema is defined.
- RULES.md contains “Git rules” and “Red lines” headings with “to be done” placeholders. These are not enforceable project policies.
- WORKFLOW.md gives the stage sequence, but does not describe handoffs, approval criteria, or how its Specification phase relates to Analysis/SPEC.md.

Do not erase or hide these gaps when exact reproduction matters. Ask the user for missing course-specific rules when they materially affect the deliverable. Otherwise use the lightweight defaults below and label them “portable working structure” or “recommended default”, not “original course standard”.

## Selecting the active project and documentation root

Begin at the user's current working directory and inspect the surrounding structure. Establish whether it is the repository root, a product subtree, or one project within a monorepo by checking the Git root, README locations, source directories, and existing docs. The task context determines which project subtree is active; do not assume the repository root is always the target.

Prefer the documentation root and naming convention already used for that target. If none exists, use docs/ under the active project root. A project/ directory is optional: place docs beneath it only when the actual repository uses that subtree for the target project or the user explicitly requests the exact source layout. Never create project/ solely to match this example. When several candidate roots remain and choosing one would create a duplicate or scope the work to the wrong project, ask the user before writing files.

The paths in the following map document the source workspace, not a universal destination. Use their relative artifact names under the documentation root selected from the current workspace structure.

The source-compatible logical map is:

| Logical artifact | Source-compatible path | Purpose |
| --- | --- | --- |
| Workflow | project/docs/WORKFLOW.md | Stage order, handoffs, and review gates |
| Rules | project/docs/RULES.md | Only user-approved repository/project rules |
| Product requirements | project/docs/Analysis/PRD.md | User/problem/scope/outcome decisions |
| Technical system spec | project/docs/Analysis/SPEC.md | Technical realization and PRD traceability |
| Research | project/docs/Research/RESEARCH.md | Evidence gathered for important decisions |
| Design structure | project/docs/Design/STRUCTURE.md | System context, elements, responsibilities, flows |
| Analysis assets | project/docs/Analysis/assets/ | Diagrams referenced by PRD or SPEC |
| Research assets | project/docs/Research/assets/ | Research figures/tables that need separate files |
| Design assets | project/docs/Design/assets/ | Context, architecture, or flow diagrams |
| Specification assets | project/docs/Specification/assets/ | Assets for separately requested detailed specification |

The source pattern includes the four assets directories. Create them when reproducing that layout. For other projects, add an assets directory only where referenced files need a home or the established convention requires one. Empty directories may require a repository's usual placeholder mechanism to be retained in version control.

## Recommended lightweight skeletons for unspecified documents

These outlines are pragmatic, project-independent defaults. They are not claims about the original course.

### Research

- Research question and decision it informs
- Scope and method
- Sources and access dates
- Findings, with evidence separated from interpretation
- Comparison of relevant options or prior art
- Implications for PRD, Design, or SPEC
- Limitations, assumptions, and open questions
- Change log

For each finding, include enough source information that another agent can verify it. Treat volatile product, law, pricing, compatibility, and market claims as time-sensitive and verify them from current authoritative sources where tools permit.

### Design structure

- Purpose and scope
- System boundary and external actors/systems
- Major components/elements and responsibilities
- Key interactions, flows, and trust/data boundaries when relevant
- Diagram links with a sentence explaining what each diagram communicates
- Design decisions linked to a PRD need, research finding, or explicit constraint
- Decisions deferred to SPEC and open questions
- Change log

Keep this a structural explanation. Avoid duplicating every interface or database field that belongs in SPEC.

### Specification directory

The source pattern provides Specification/assets/ but does not define a document inside it. Leave it as an asset location unless the user asks for a separate detailed specification or a project convention requires one. Do not duplicate Analysis/SPEC.md. If the user asks for a detailed specification, first identify the intended audience, scope, and relation to the system SPEC; then name and outline it transparently.

## Workflow and handoff interpretation

The observed stage sequence is Analysis → Research → Design → Specification. The detailed prompts also require a PRD → SPEC mapping and specify Analysis/SPEC.md as the SPEC destination. A practical interpretation is:

1. Analysis establishes and stabilizes the PRD.
2. Research gathers evidence needed to resolve important product/design/engineering questions.
3. Design describes the solution structure at a high level.
4. Specification records technical contracts and dispositions against the PRD, in the source-compatible path Analysis/SPEC.md unless the repository has a different established path.

This reconciles phase order and filename without pretending the source explicitly documented the handoffs. If reproducing the original course process exactly, show this as an interpretation and ask the user whether they want to resolve the ambiguity. For ordinary project work, use the interpretation and keep the uncertainty visible in WORKFLOW.md.

## Rules and temporary branch process

The source RULES.md does not define actual Git rules or red lines. Do not convert the temporary-branch instructions in the two prompts into permanent repository policy unless the user asks to record them there. Those instructions govern the relevant PRD/SPEC workstream. Remote branch creation or deletion remains subject to the active environment's authorization and repository rules.
