Inspect the folder or file specified in $ARGUMENTS and write or update `docs/interviewnotes.md` with everything the user must understand to confidently discuss this code in a technical interview.

The goal is not to explain what the code does — the user wrote it. The goal is to ensure they can answer follow-up questions under pressure: why it was designed this way, what happens in edge cases, what the tradeoffs are, how it could fail, and how it could be extended.

---

Structure `docs/interviewnotes.md` with one top-level section per file or package analyzed. If the file already exists, update or extend the relevant sections rather than rewriting the whole document.

For each file or package, cover the following:

## 1. High-Level Purpose
What is this component's role in the system and why does it exist? What would break or be impossible without it?

## 2. Core Concepts
List the main algorithms, patterns, data structures, or design decisions the user must be able to name and explain. Don't just describe — explain the "why" behind each one.

## 3. Important Implementation Details
Cover:
- Key functions, structs, interfaces, or types and what they do
- Important data flow (what goes in, what comes out, what changes in Redis or elsewhere)
- Dependencies and why they were chosen
- Environment variables or configuration that affects behavior
- Any syntax or API usage that is non-obvious and worth remembering

## 4. Interview Questions
Write the actual questions an interviewer might ask, followed by a concise model answer. Include:
- How does it work?
- Why was it built this way?
- What alternatives exist and why were they not chosen?
- How could it fail?
- How could it be improved?
- How does it scale?

Format each as:
**Q: [question]**
A: [answer]

## 5. Edge Cases and Failure Modes
Identify tricky behavior, subtle bugs, race conditions, incorrect assumptions, or performance cliffs the user should be aware of. For each, explain what happens and why.

## 6. Modification Scenarios
Explain how a realistic change would be made:
- Adding a new algorithm or feature
- Changing the architecture
- Improving performance
- Swapping a dependency
- Supporting more scale or more reliability

Be specific — name the files and functions that would change.

## 7. Must-Know Summary
End each section with 4–8 bullet points the user must be able to explain out loud without hesitation. These should be the most interview-critical facts about the component.

---

Guidelines

- Prioritize interview readiness. A candidate reading this should feel prepared, not just informed.
- Be specific and practical. Avoid generic advice.
- Explain reasoning, not just behavior. "Why" matters more than "what."
- Include non-obvious syntax or API usage when it would trip someone up in an interview.
- Inspect surrounding files and context when needed to give complete answers.
- If $ARGUMENTS is a folder, inspect all relevant files recursively.
- If $ARGUMENTS is a file, read related files for context where helpful.
- Keep writing dense and concise. No fluff.
