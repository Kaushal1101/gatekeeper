The user may optionally provide additional instructions in $ARGUMENTS describing areas they would like reviewed or improved. Treat these instructions as additional priorities while still performing a holistic review of the codebase.

Before beginning any refactoring:

1. Verify that the current implementation appears complete and functionally stable.
2. Check whether there are uncommitted changes.
3. If the current work has not been safely committed (or the user has not explicitly accepted that risk), recommend creating a commit before proceeding with the refactor.
4. Do not create commits yourself.

Once it is safe to proceed, refactor the codebase with the following goals.

Code Quality

* Improve readability and maintainability.
* Remove dead or obsolete code.
* Reduce duplication.
* Simplify overly complex logic where appropriate.
* Improve naming consistency.
* Preserve existing behaviour unless the user explicitly requests otherwise.

Project Structure

Review the overall organization of the repository.

Where appropriate:

* Reorganize folders.
* Improve package/module boundaries.
* Separate responsibilities.
* Move code into more appropriate locations.
* Improve configuration organization.
* Introduce missing documentation files if beneficial.
* Improve consistency across the project.

Avoid unnecessary restructuring that provides little practical value.

Architecture

Look for opportunities to improve architecture by:

* Reducing coupling.
* Improving separation of concerns.
* Introducing interfaces or abstractions where justified.
* Removing legacy patterns that no longer serve the project.

Do not introduce design patterns simply for the sake of using them.

Documentation

Update any affected documentation so it reflects the refactored structure.

If architecture or project organization changes significantly, update the relevant documentation.

Legacy Code

Identify and remove:

* obsolete code
* unused files
* redundant configuration
* outdated comments
* deprecated implementations
* temporary development artifacts

Only remove code when you are confident it is no longer required.

User-Specified Focus

If $ARGUMENTS contains additional instructions, treat those as higher priority while still ensuring the overall quality of the codebase.

Final Report

After the refactor, provide a concise summary including:

* Major structural changes
* Files added, removed, or relocated
* Important architectural improvements
* Any areas intentionally left unchanged and why
* Any risks or follow-up work that should be considered

Important Guidelines

* Preserve functionality throughout the refactor.
* Prefer incremental, understandable improvements over unnecessary rewrites.
* Do not change public behaviour unless explicitly requested.
* Do not commit the refactor.
* At the end, recommend that the user:
    1. Review the changes.
    2. Run the project's tests and benchmarks.
    3. Perform any necessary manual validation.
    4. Create a commit only after confirming everything works correctly.
