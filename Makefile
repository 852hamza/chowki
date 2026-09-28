# Project identity (PROJECT_DOMAIN, GITHUB_OWNER, GITHUB_REPO, WEBSITE_URL,
# DOCS_URL). After editing project.env, run `make sync`.
include project.env

GO ?= go

# Same rule as internal/buildinfo and tools/projectsync.
GO_MODULE := github.com/$(GITHUB_OWNER)/$(GITHUB_REPO)
BUILDINFO := $(GO_MODULE)/internal/buildinfo

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null)
COMMIT  ?= $(shell git rev-parse HEAD 2>/dev/null)
DATE    ?= $(shell TZ=UTC0 git show -s --date=format-local:%Y-%m-%dT%H:%M:%SZ --format=%cd HEAD 2>/dev/null)

# Evaluated only by `build` and `docker`, so other targets don't run git.
LDFLAGS = \
	-X $(BUILDINFO).version=$(VERSION) \
	-X $(BUILDINFO).commit=$(COMMIT) \
	-X $(BUILDINFO).date=$(DATE) \
	-X $(BUILDINFO).domain=$(PROJECT_DOMAIN) \
	-X $(BUILDINFO).owner=$(GITHUB_OWNER) \
	-X $(BUILDINFO).repo=$(GITHUB_REPO) \
	-X $(BUILDINFO).website=$(WEBSITE_URL) \
	-X $(BUILDINFO).docs=$(DOCS_URL)

# Extra linker flags. The container image passes -s -w, which leave out the
# symbol tables.
EXTRA_LDFLAGS ?=

# The tag that `make docker` gives the image, as deploy/compose.yaml names it.
IMAGE ?= chowki:dev

.PHONY: build docker test race lint vuln sync sync-check docs loadtest help

## build: compile bin/chowki with version and project identity
build:
	$(GO) build -trimpath -ldflags '$(LDFLAGS) $(EXTRA_LDFLAGS)' -o bin/chowki ./cmd/chowki

## docker: build the container image $(IMAGE) from deploy/Dockerfile
docker:
	docker build -f deploy/Dockerfile --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) \
		--build-arg DATE=$(DATE) -t $(IMAGE) .

## test: run all tests
test:
	$(GO) test ./...

## race: run all tests with the race detector
race:
	$(GO) test -race ./...

## lint: run golangci-lint (configured in .golangci.yml)
lint:
	golangci-lint run

## vuln: check dependencies and the Go standard library for known vulnerabilities
vuln:
	govulncheck ./...

## sync: apply project.env to go.mod, imports, docs and buildinfo defaults
sync:
	$(GO) run ./tools/projectsync

## sync-check: fail if the repository is out of sync with project.env
sync-check:
	$(GO) run ./tools/projectsync --check

## docs: rewrite the parts of docs/reference that are generated from the code
docs:
	UPDATE_DOCS=1 $(GO) test -count=1 -run 'TestCLIReference|TestConfigurationSummary|TestMetricsSummary|TestErrorCodesReference' \
		./cmd/chowki ./internal/config ./internal/metrics ./internal/pipeline

## loadtest: measure the gateway's overhead at 200 requests per second on 2 CPUs
loadtest: build
	$(GO) run ./tools/loadtest --binary bin/chowki

## help: list these targets
help:
	@sed -n 's/^## //p' Makefile
