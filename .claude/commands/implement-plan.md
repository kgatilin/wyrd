# Implement Plan with Subagent Orchestration

This command implements a plan file using orchestrated subagents with validation cycles.

## Usage

```
/implement-plan <path-to-plan.md>
```

## Arguments

- `$ARGUMENTS` - Path to the plan file (e.g., `docs/features/us1-generate-diagrams-split/plan.md`)

## Workflow

For each iteration in the plan:

1. **Implementation Phase**
   - Use `world-class-engineer` subagent for complex architectural work
   - Use `junior-dev-executor` subagent for straightforward tasks
   - Decision criteria:
     - World-class-engineer: Domain models, adapters, service layer, architectural decisions
     - Junior-dev-executor: Simple file creation, boilerplate, configuration files

2. **Validation Phase**
   - Run `validate-iteration` subagent to check implementation against acceptance criteria
   - Validates that all tasks in the iteration are complete and correct

3. **Fix Phase** (if needed)
   - If validation finds failures, run `fix-iteration` subagent
   - Re-validate after fixes

4. **Iteration Control**
   - Maximum 3 validation cycles per iteration
   - If still failing after 3 attempts, STOP and notify user with details
   - Move to next iteration only when current passes validation

## Implementation Instructions

When this command is invoked:

1. **Read the plan file** at `$ARGUMENTS`

2. **Parse the iterations** - Identify each iteration block (e.g., "Iteration 1: Project Foundation")

3. **For each iteration**, execute this loop:

```
validation_attempts = 0
MAX_ATTEMPTS = 3

while validation_attempts < MAX_ATTEMPTS:
    if validation_attempts == 0:
        # First time - implement the iteration
        Choose appropriate subagent:
        - world-class-engineer: for domain models, adapters, services, architectural code
        - junior-dev-executor: for simple config, boilerplate, straightforward tasks

        Launch subagent with prompt:
        "Implement Iteration N from the plan at {plan_path}.
         Focus on these tasks: {list tasks from iteration}.
         Follow the plan specifications exactly."

    # Validate
    validation_attempts++
    Launch validate-iteration subagent

    if validation passes:
        break  # Move to next iteration
    else if validation_attempts < MAX_ATTEMPTS:
        # Fix failures
        Launch fix-iteration subagent
        # Loop continues to re-validate
    else:
        # Max attempts reached
        STOP execution
        Report to user:
        - Which iteration failed
        - What acceptance criteria failed
        - How many fix attempts were made
        - Ask user for guidance
```

4. **Progress Reporting**
   - Before each iteration: "Starting Iteration N: {name}"
   - After implementation: "Implementation complete, validating..."
   - After validation: "Validation passed" or "Validation failed: {issues}"
   - After fixes: "Fixes applied, re-validating (attempt N/3)"

5. **Completion**
   - When all iterations complete: Summary of what was built
   - If stopped early: Clear explanation of where it stopped and why

## Subagent Selection Guide

### Use `world-class-engineer` for:
- Domain model definitions (structs, interfaces, value objects)
- Adapter implementations (readers, writers)
- Service layer code
- Code requiring architectural judgment
- Complex type systems and dependency management
- Integration between components

### Use `junior-dev-executor` for:
- Creating directory structure
- go.mod and go.sum initialization
- Simple configuration files
- Placeholder files with TODO comments
- Copy-paste style boilerplate
- Simple CLI scaffolding

## Error Handling

- If a subagent fails to complete, report the error and stop
- If validation consistently fails on the same criteria, include that detail
- Preserve all error context for user debugging

## Example Execution

```
User: /implement-plan docs/features/us1-generate-diagrams-split/plan.md

Claude: Starting implementation of US-1 Generate Diagrams - Split Mode

=== Iteration 1: Project Foundation ===
Launching junior-dev-executor for directory structure and go.mod setup...
[subagent completes]
Implementation complete, validating...
[validate-iteration runs]
Validation passed!

=== Iteration 2: Go Adapter (Reader) ===
Launching world-class-engineer for adapter implementation...
[subagent completes]
Implementation complete, validating...
[validate-iteration runs]
Validation failed: Task 2.3 - Symbol extraction missing method receiver handling
Launching fix-iteration...
[fix-iteration completes]
Re-validating (attempt 2/3)...
[validate-iteration runs]
Validation passed!

... continues for all iterations ...

=== Summary ===
Successfully implemented 6 iterations:
- Iteration 1: Project Foundation (1 validation cycle)
- Iteration 2: Go Adapter (2 validation cycles)
- Iteration 3: D2 Adapter (1 validation cycle)
- Iteration 4: Service Layer (1 validation cycle)
- Iteration 5: CLI (1 validation cycle)
- Iteration 6: Testing (1 validation cycle)
```

## Notes

- The plan file path should be relative to the workspace root
- Each iteration is implemented atomically - partial iterations are avoided
- Validation uses the acceptance criteria defined in the plan
- All code follows the architectural patterns specified in the plan