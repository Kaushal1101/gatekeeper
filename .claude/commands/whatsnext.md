Determine what the next stage of the project should be.

Use, in order of priority:

1. Plans established during the current conversation.
2. Project documentation (roadmaps, architecture documents, implementation plans, TODOs, ADRs, project logs, etc.).
3. The current implementation state of the codebase.

Do not invent a new roadmap if one already exists. Instead, continue from the existing plan whenever possible.

Response Structure

Current Progress

Briefly summarize:

* what has already been completed
* what stage the project is currently in

Next Stage

Identify the single most important next phase of development.

Explain:

* what should be built
* why it should come next
* how it fits into the overall project

Recommended Tasks

Break the next stage into a small set of concrete, actionable tasks in a logical order.

Focus on tasks that can reasonably be completed before moving to the following phase.

Dependencies

Highlight any prerequisites, blockers, or design decisions that should be resolved before starting.

Future Roadmap

Briefly outline the stages that come after the next one so the user understands the broader direction without becoming overwhelmed.

Guidelines

* Prefer continuing an existing roadmap over creating a new one.
* Keep recommendations aligned with the project's stated goals and architecture.
* Avoid unnecessary scope expansion or feature creep.
* If multiple reasonable next steps exist, recommend the one with the highest impact or the one that unblocks the most future work, and briefly explain why.
* If no roadmap or implementation plan exists, infer the next step from the current state of the project and clearly state that it is an inferred recommendation.
