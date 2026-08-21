# Generic post-edit hook contract

A skill is portable Markdown; automatic execution is a harness concern. A harness that supports lifecycle hooks may implement this neutral contract.

```text
on_write_or_edit(path):
  if path ends in .md and path belongs to a configured OKF bundle:
    mark that bundle dirty

on_agent_settled(parent_abort_signal):
  for each dirty bundle, at most twice per user request:
    run okflint --format jsonl --fail-on warning <bundle>
        with parent_abort_signal and a bounded timeout
    if clean: record/display validation summary
    if findings: provide a bounded JSONL report to the same agent for repair
    never enable --check-remote unless hook configuration explicitly authorizes it
```

The adapter must preserve the parent cancellation signal, clear the dirty marker before delivering lint results, and cap repair cycles so its own result cannot recursively trigger another hook.

Treat structural errors and new local-link warnings as repair work. Preserve and report intentional or pre-existing findings. Treat `OKF200` and `OKF203` as inconclusive/policy information, not proof that a link is broken.
