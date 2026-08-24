# okf-ok

A container-first linter for [Open Knowledge Format (OKF)](https://github.com/GoogleCloudPlatform/knowledge-catalog/tree/main/okf) bundles, implemented in Go.

`okfok` is read-only and offline by default. It validates the minimum OKF v0.2 document structure and reports local Markdown and standardized frontmatter references whose targets have drifted. OKF permits broken cross-links, so missing targets are warnings rather than conformance errors.

## Run with Docker

Release images are published to GitHub Container Registry for `linux/amd64` and `linux/arm64`. Pin a digest in automation, mount the bundle read-only, and disable networking for the offline default:

```sh
export OKFOK_IMAGE='ghcr.io/mark-ht/okf-ok@sha256:<published-digest>'

docker run --rm --read-only --network none \
  -v "$PWD:/work:ro" \
  "$OKFOK_IMAGE" /work/bundle

# Machine-readable diagnostics for an agent or CI step.
docker run --rm --read-only --network none \
  -v "$PWD:/work:ro" \
  "$OKFOK_IMAGE" --format jsonl --fail-on warning /work/bundle
```

The image runs as a non-root user. It checks `resource`, `sources[].resource`, `computation`, `executor.resource`, and `attester.resource`, as well as Markdown links. Use `--check-fragments` to opt into the documented local heading-anchor policy.

Remote HTTPS health is explicit and policy-bound. Enable a network only for an authorized environment and a narrow allowlist:

```sh
# VPN-only/private destinations are intentionally not probed.
docker run --rm --read-only --network bridge \
  -v "$PWD:/work:ro" \
  "$OKFOK_IMAGE" --check-remote --remote-policy allowlist \
  --allow-host developers.google.com /work/bundle
```

Remote outcomes distinguish `gone` (404/410) and `redirected` from environment-dependent `inconclusive` (for example, VPN-only, authentication, timeout, or 5xx). Only selected outcomes fail with `--fail-on-remote=gone|redirected|all`. Do not provide credentials or a writable bundle mount unless a separate workflow requires them.

### Build locally

The Dockerfile uses a multi-stage build: Go is present only in the build stage, while the final image contains the stripped, statically linked `okfok` binary and a non-root distroless runtime.

```sh
docker build -t okfok:local .
docker run --rm --read-only --network none \
  -v "$PWD:/work:ro" okfok:local /work/bundle
```

## Generate Go knowledge bundles

`okfok` is the workspace command. It inventories local Go declarations and produces a deterministic, reviewable OKF plan; it never writes until `apply` receives that plan.

```sh
okfok generate plan --repo . --bundle knowledge --format json > .okfok/plan.json
okfok generate apply --repo . --plan .okfok/plan.json
okfok lint --repo . --bundle knowledge
```

Generation is offline and does not execute Go code. The initial generator covers declared Go package symbols and members, writes only `knowledge/`, and refuses to overwrite hand-authored output. Its source provenance may point to regular non-symlinked files under the explicit repository workspace; Markdown concept links remain within `knowledge/`.

The existing bare `okfok <bundle>` interface remains available for compatibility. The container entrypoint is now `okfok` and likewise treats a bare bundle path as legacy lint usage.

## Relationship viewer

Serve a read-only progressive relationship viewer. It opens with aggregate package/directory relationships, then uses search, tree navigation, and bounded selected-document neighborhoods instead of rendering an unreadable full-corpus graph:

```sh
okfok --serve 127.0.0.1:8080 ./bundle
# Open http://127.0.0.1:8080
```

Clicking a heading loads that heading's bounded Markdown section into the side panel; "Open document source" serves the full document as safe plain text. The viewer never performs remote link checks or loads third-party web assets. It is deliberately a local server: select a loopback address unless access from another host is intentional.

To serve a pinned remote repository through the container, clone it on the host first, mount it read-only, and publish only a loopback port. The bridge network permits the browser's inbound connection; `okfok` makes no outbound requests in viewer mode.

```sh
git clone https://github.com/finos/morphir-scala.git
git -C morphir-scala checkout 4b2f76f15fd3a241037323e8f92624f39430ed37

docker run --rm --read-only --network bridge \
  -p 127.0.0.1:8080:8080 \
  -v "$PWD/morphir-scala:/work:ro" \
  "$OKFOK_IMAGE" \
  --serve 0.0.0.0:8080 /work/kb/bundles/morphir/morphir-ir-v3
```

Open `http://127.0.0.1:8080`. Direct Git URL fetching is intentionally unsupported: pin and review the checkout outside the minimal runtime image.

## GitHub Action

The composite action runs a release image with a read-only workspace mount. Pin the image digest rather than a mutable tag:

```yaml
- uses: mark-ht/okf-ok/.github/actions/okfok@v1
  with:
    image: ghcr.io/mark-ht/okf-ok@sha256:<published-digest>
    bundle-path: knowledge
    fail-on: warning
```

It defaults to `--network none`, writes a JSONL report to `.okfok/report.jsonl`, and exposes the exit code, report path, and finding count as action outputs. Enable remote checks only on an explicitly trusted runner with an allowlist.

## Automation contract

The default text output ends with a deterministic summary table: discovered/read Markdown files, concept and reserved documents, checked references, document `type` counts, and findings grouped by severity and diagnostic code. `--format jsonl` emits one `okf.lint/v1` diagnostic per line and deliberately omits the summary so it remains stream-safe. Each diagnostic has a stable code, severity, bundle-relative file and source position, reference kind, optional field, target, resolved local target, outcome, and message. Output is sorted by file, position, code, and target. JSON emits the same ordered diagnostics as an array.

Exit status is `0` when no selected findings exist, `1` for diagnostics selected by `--fail-on` or `--fail-on-remote`, `2` for invalid invocation or an unreadable root, `3` for an output/internal failure, and `130` for cancellation. Bare `sources[].resource` values that might be scope descriptors are informational by default; use `--strict-source-paths` to require them to resolve locally.

Use `--format sarif` to produce a deterministic SARIF 2.1.0 report for code-scanning annotations:

```yaml
- run: >-
    docker run --rm --read-only --network none
    -v "$PWD:/work:ro" "$OKFOK_IMAGE"
    --format sarif /work/knowledge > okfok.sarif
- uses: github/codeql-action/upload-sarif@v3
  with:
    sarif_file: okfok.sarif
```

## Agent skill

The tool-neutral [Agent Skills](https://agentskills.io)-compatible package is at [`skills/okf-lint/`](skills/okf-lint/). It contains only standard Markdown skill instructions and can be added to any compatible agent's skill search path without a harness-specific extension.

The skill defaults to offline JSONL linting after an OKF edit and includes a generic, bounded post-edit/post-turn lifecycle contract for harnesses that support hooks. See [`skills/README.md`](skills/README.md) for installation and portability guidance.

## Development

```sh
go test ./...
go test -race ./...
go vet ./...
test -z "$(gofmt -l .)"
```
