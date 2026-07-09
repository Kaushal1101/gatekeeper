The goal of this command is not to summarize the entire project. Instead, summarize only the most recent discussion, implementation work, or decision-making that immediately preceded the current point of development.

Determine the summary using the following sources, in order of priority:

1. The current conversation context.
2. A persistent "last conversation" summary file at .claude/session-summary.md.
3. The most recent project log or documentation update, if neither of the above is available.

Response Structure

Where We Left Off

Provide a short summary (2–5 paragraphs) explaining:

* what we were working on
* what was accomplished
* what decisions were made
* any important conclusions reached

Current Context

Summarize:

* the current implementation state
* what the immediate focus was
* anything that was intentionally deferred

Important Decisions

List the key engineering or architectural decisions made during the recent discussion.

Only include decisions that are relevant going forward.

Immediate Next Step

State the very next thing the user should work on to continue naturally from where they stopped.

Session Summary File

After providing the summary, update .claude/session-summary.md with only the information necessary to resume work quickly:

* Current task
* Recent implementation
* Recent architectural decisions
* Outstanding questions
* Immediate next step

Keep it concise (roughly one page or less). This file should not be committed to version control — ensure .claude/session-summary.md is listed in .gitignore.

Guidelines

* Focus only on the most recent work, not the full project history.
* Avoid repeating information already covered in long-term project documentation.
* Keep the summary concise, practical, and action-oriented.
* If no previous context exists, clearly state that and summarize the current project state as best as possible from the available documentation.
