---
description: Implementation Principles, ask LLM to follow this spec with `implemente feature ...` to generate the code.
---
# Main Tasks
Read specs in the same directory, including:
- Read `docs/specs/<spec_dir>/impl_details/requirements.md` to understand the user requirements.
- Read `docs/specs/<spec_dir>/impl_details/plan.md` to get the implementation plan.
- Follow `docs/specs/<spec_dir>/impl_details/tasks.md` to generate code and implement features.
- Create implementation details in the `docs/specs/<spec_dir>/impl_details/` directory under this feature spec directory. 
- After the implementation is complete and verified, update `docs/specs/<spec_dir>/impl_details/task_process.md` by changing the status of the corresponding task to DONE.
- Continue remaining tasks according to `docs/specs/<spec_dir>/impl_details/tasks.md` and  `docs/specs/<spec_dir>/impl_details/task_process.md`.
The implementation must follow the Principles sections.


# Principles
## Library-First Principle
Every feature in Specify MUST begin its existence as a standalone library.
No feature shall be implemented directly within application code without
first being abstracted into a reusable library component.

## CLI Interface Mandate
All CLI interfaces MUST:
- Accept text as input (via stdin, arguments, or files)
- Produce text as output (via stdout)
- Support JSON format for structured data exchange

## Test-First Principle
This is NON-NEGOTIABLE: All implementation MUST follow strict Test-Driven Development.
No implementation code shall be written before:
1. Unit tests are written
2. Tests are validated and approved by the user
3. Tests are confirmed to FAIL (Red phase)

# File Creation Order
1. Create `contracts/` with API specifications
2. Create test files in order: contract → integration → e2e → unit
3. Create source files to make tests pass
