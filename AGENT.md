# Requirement Rules
If you are asking to design a new requirement, you must follow:
You are currently developing a large and complex task. 
- Docs named `docs/specs/<spec_dir>/requirements.md` describes the task requirements, you must read it carefully before working.
- A large task must be break down into smaller sub-tasks (and make adjustments according to your latest knowledge).
- For each sub-task, you need to write detailed work plan in `docs/specs/<spec_dir>/plan.md` file.
- If some sub-task is complex after dig deeper, you can further break it down into smaller tasks in the work plan.

You also need to track your development progress in `docs/specs/<spec_dir>/impl_details/task_progress.md`, so that you can effectively manage your development without losing sight of the overall goals.
During you explore the code base, you must organize what you have learned in `docs/knowledge` directory using .md files:
- You must keep facts, details and code locations in there, instead of summarizing or giving high-level overviews.
- Before starting working, you must also try to read from existing knowledge.

If you find all task items in your work plan are completed in `docs/specs/<spec_dir>/impl_details/task_progress.md`, then you should start a new task:
- Carefully read the current changes (in git diff and in staging area) you have made so far, compare them with your work plan, and verify that all critical components in the design document have been implemented, and think what else can be improved in engineerings aspects (code quality, performance, readability, modularity, test coverage, etc). We seek for production-quality code, high performance and correctness. Your new discoveries and improvements should also be recorded in `docs/specs/<spec_dir>/plan.md`.
Finally, never ask me any questions. You decide everything by yourself. You can always explore the code base and read from existing knowledge to resolve your doubts.

Always use Context7 MCP when I need library/API documentation, code generation, setup or configuration steps without me having to explicitly ask.
