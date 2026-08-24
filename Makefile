IMAGE ?= okfok:local
REPO ?= .
BUNDLE ?= knowledge
PORT ?= 8080

REPO_ABS := $(abspath $(REPO))
USER_ID := $(shell id -u):$(shell id -g)
PLAN_HOST := $(REPO_ABS)/.okfok/plan.json
PLAN_CONTAINER := /work/.okfok/plan.json

.PHONY: help build generate-plan generate-apply lint serve test check

help:
	@printf '%s\n' \
	  'okfok Docker workflow:' \
	  '  make build' \
	  '  make generate-plan REPO=/path/to/go-repository' \
	  '  make generate-apply REPO=/path/to/go-repository' \
	  '  make lint REPO=/path/to/go-repository' \
	  '  make serve REPO=/path/to/go-repository PORT=8080' \
	  '' \
	  'Variables: IMAGE=$(IMAGE) REPO=$(REPO) BUNDLE=$(BUNDLE) PORT=$(PORT)'

build:
	docker build -t $(IMAGE) .

generate-plan:
	mkdir -p "$(REPO_ABS)/.okfok"
	@set -e; temp="$(PLAN_HOST).tmp"; \
	docker run --rm --read-only --network none \
	  -v "$(REPO_ABS):/work:ro" \
	  $(IMAGE) generate plan --repo /work --bundle "$(BUNDLE)" --format json > "$$temp"; \
	mv "$$temp" "$(PLAN_HOST)"
	@printf 'Wrote reviewable plan: %s\n' "$(PLAN_HOST)"

generate-apply:
	docker run --rm --read-only --network none \
	  --user "$(USER_ID)" \
	  -v "$(REPO_ABS):/work:rw" \
	  $(IMAGE) generate apply --repo /work --plan "$(PLAN_CONTAINER)"

lint:
	docker run --rm --read-only --network none \
	  --user "$(USER_ID)" \
	  -v "$(REPO_ABS):/work:ro" \
	  $(IMAGE) lint --repo /work --bundle "$(BUNDLE)"

serve:
	docker run --rm --read-only --network bridge \
	  --user "$(USER_ID)" \
	  -p 127.0.0.1:$(PORT):8080 \
	  -v "$(REPO_ABS):/work:ro" \
	  $(IMAGE) serve --repo /work --bundle "$(BUNDLE)" 0.0.0.0:8080

test:
	go test ./...
	go test -race ./...
	go vet ./...

check: test
	test -z "$$(gofmt -l .)"
	git diff --check
