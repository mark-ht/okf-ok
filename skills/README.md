# Agent Skills

`skills/okf-lint/` is the canonical, tool-neutral [Agent Skills](https://agentskills.io) package for this project. It follows the standard `SKILL.md` directory layout and contains no harness extension code.

Use it with any Agent Skills-compatible tool by adding this repository's `skills/` directory to that tool's skill search path, or by copying the entire `okf-lint/` directory without renaming it. The only runtime dependency is `okfok` (or the documented digest-pinned container image).

Examples:

```sh
# Pi: load explicitly without relying on project-local configuration.
pi --skill ./skills/okf-lint
```

For harnesses that support post-edit or post-turn hooks, implement the generic lifecycle contract in [`okf-lint/references/post-hook.md`](okf-lint/references/post-hook.md). The contract deliberately defaults to local offline linting and requires explicit authorization for remote checks.
