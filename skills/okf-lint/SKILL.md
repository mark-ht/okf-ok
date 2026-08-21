---
name: okf-lint
description: Validate Open Knowledge Format bundles after creating or editing Markdown knowledge documents. Use before completing an OKF-related task to find malformed frontmatter and local or authorized remote reference drift.
license: Apache-2.0
compatibility: Requires the okfok CLI or a digest-pinned OCI image. Offline validation needs no network access.
---

# OKF lint

Run this skill after editing an OKF bundle and before reporting completion. `okfok` is read-only. It distinguishes structural OKF errors from warning-level link health: OKF permits broken cross-links, but newly introduced warnings need repair or an explicit explanation.

## Safe default

Locate the bundle root containing the edited document, then run the offline checker with machine-readable output:

```sh
okfok --format jsonl --fail-on warning <bundle-root>
```

If the binary is unavailable, use a reviewed, digest-pinned release image:

```sh
docker run --rm --network none -v "$PWD:/work:ro" \
  ghcr.io/mark-ht/okf-ok@sha256:<published-digest> \
  --format jsonl --fail-on warning /work/<bundle-root>
```

Do not use a mutable image tag in CI or agent automation.

## Interpret results

- `OKF010`–`OKF012`: fix malformed/missing frontmatter or the required `type` before completion.
- `OKF100`: fix a bundle escape or symlink traversal. Never work around it by accessing paths outside the bundle.
- `OKF101`: fix the local path or state explicitly that the permitted broken link is intentional.
- `OKF103`: a source may be a scope descriptor rather than a path. Do not rewrite it unless the task establishes that it should resolve locally. Use `--strict-source-paths` only when the repository policy requires it.
- `OKF104`: use `--check-fragments` only when the project adopts the linter's documented heading-anchor policy.
- `OKF200` / `OKF203`: remote state is inconclusive or was skipped; do not infer that a VPN-only, authenticated, or policy-restricted URL is broken.
- `OKF201`: a remote URL returned 404/410 from the authorized environment; repair or report it.
- `OKF202`: a redirect is drift evidence; update the canonical URL only when appropriate.

## Remote checks

Remote probing is never implicit. Use it only with user-approved hosts and an environment authorized to reach them:

```sh
okfok --format jsonl --check-remote --remote-policy allowlist \
  --allow-host docs.example.com --fail-on-remote gone <bundle-root>
```

Never add credentials, bypass VPN restrictions, use private-network hosts, or select `--remote-policy public` without explicit user approval.

## Completion report

State the exact command, bundle root, and result. For example:

```text
Validated `knowledge/` with `okfok --format jsonl --fail-on warning knowledge`:
0 structural errors; 0 link warnings.
```

If pre-existing or intentional findings remain, name their codes and paths rather than claiming a clean lint result.

See [post-hook guidance](references/post-hook.md) when a harness can run validation automatically after an edit batch.
