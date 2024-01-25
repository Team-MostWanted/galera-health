GO			?= go
GOOPTS		?=
GOFMT		?= gofmt
STATICCHECK	?= staticcheck
PACKAGES	?= ./...
CHANGELOG	?= CHANGELOG.md

BIN_DIR		?= ./bin
BUILD_DIR	?= ./build
DIST_DIR	?= ./dist

APPNAME		?= $(shell perl -ne 'if (/^module /) { s/^module .+\///; print; }' go.mod)
VERSION		?= $(shell perl -lne 'if (/^\#\# \[\d+\.\d+\.\d+\] - [^\[A-Za-z]/) { print substr((split(/\s+/))[1], 1, -1); last; }' $(CHANGELOG))
COMMIT		?= $(shell git rev-parse --verify HEAD)
DATE		:= $(shell date +%FT%T%z)
TARGETS		:= freebsd/amd64 darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64

GO_LDFLAGS	+= -X main.name=$(APPNAME)
GO_LDFLAGS	+= -X main.version=v$(VERSION)
GO_LDFLAGS	+= -X main.commit=$(COMMIT)
GO_LDFLAGS	+= -X main.date=$(DATE)
# strip debug info from binary
GO_LDFLAGS 	+= -s -w

GO_LDFLAGS 	:= -ldflags="$(GO_LDFLAGS)"

VERSION_NEXT 	?= $(shell echo $(word 1, $(subst ., ,$(VERSION))).$$(($(word 2, $(subst ., ,$(VERSION))) + 1)).0)
CHANGELOG_LINE	?= - Security: dependency and security updates

ifeq (,$(shell sed -nE '/\#\# .*(upcoming|unreleased)/Ip' $(CHANGELOG)))
	CHANGELOG_LINES ?= \#\# \[$(VERSION_NEXT)\] - $(shell date +%F)\n$(CHANGELOG_LINE)\n\n
else
	CHANGELOG_LINES ?= $(CHANGELOG_LINE)\n\n
endif

.PHONY: version
version:
	@echo "$(VERSION)"

.PHONY: test
test:
	@echo "Start test for: $(APPNAME) v$(VERSION)"

	test -z "$$($(GOFMT) -l .)" # Check Code is formatted correctly
	$(GO) mod verify # ensure that go.sum agrees with what's in the module cache
	$(GO) vet $(PACKAGES) # examines Go source code and reports suspicious constructs

ifneq (,$(shell which $(STATICCHECK)))
	$(STATICCHECK) $(PACKAGES) # extensive analysis of Go code
endif

	mkdir -p $(BUILD_DIR)
	# run unit test with coverage
	$(GO) test -race -coverprofile=$(BUILD_DIR)/test-coverage.out -json $(PACKAGES) > $(BUILD_DIR)/test-report-unit.json
	# run benchmark test once to make sure they work
	$(GO) test -run=- -bench=. -benchtime=1x -json $(PACKAGES) > $(BUILD_DIR)/test-report-benchmark.json

.PHONY: build
build:
	@echo "Start build for: $(APPNAME) v$(VERSION)"

	@if grep -q "package main" *.go 2>/dev/null ; then \
		mkdir -p $(BUILD_DIR); \
		for target in $(TARGETS); do \
			os=$$(echo $$target | cut -d/ -f1); \
			arch=$$(echo $$target | cut -d/ -f2); \
			CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build $(GO_LDFLAGS) -o $(BUILD_DIR)/$(APPNAME)-$(VERSION)-$$os-$$arch/ $(PACKAGES); \
		done; \
	else \
		echo "No building required, module does not contain the 'main' package."; \
    fi

.PHONY: dist-check
dist-check:
ifneq (,$(filter ${GITHUB_REF\#refs/heads/},release/acceptance master))
	@echo "Performing dist check for: ${GITHUB_REF\#refs/heads/}"

	@if ! test -z "$$(sed -E -n '/(upcoming|unreleased)/I,/##/p' changelog.md | sed '1d;$$d' | sed 's/[[:space:]-]//g')"; then \
		echo "Error: cannot generate dist, changelog.md must not contain unreleased lines."; \
		exit 1; \
	fi

	# Check for changes in go.mod or go.sum
	@mkdir -p $(BUILD_DIR)
	@cp go.mod $(BUILD_DIR)/go.mod.chk
	@cp go.sum $(BUILD_DIR)/go.sum.chk

	test -z $$($(GO) mod tidy)

	diff go.mod $(BUILD_DIR)/go.mod.chk
	diff go.sum $(BUILD_DIR)/go.sum.chk

	@rm $(BUILD_DIR)/go.mod.chk $(BUILD_DIR)/go.sum.chk
else
	@echo "Skipping dist check."
endif

.PHONY: dist-create
dist-create:
	@echo "Create dist for: $(APPNAME) v$(VERSION)"

	@if grep -q "package main" *.go 2>/dev/null ; then \
		mkdir -p $(DIST_DIR); \
		for target in $(TARGETS); do \
			os=$$(echo $$target | cut -d/ -f1); \
			arch=$$(echo $$target | cut -d/ -f2); \
			tar -C $(BUILD_DIR) -cvzf $(DIST_DIR)/$(APPNAME)-$(VERSION)-$$os-$$arch.tar.gz $(APPNAME)-$(VERSION)-$$os-$$arch; \
		done; \
	fi

.PHONY: dist
dist: clean dist-check build dist-create

.PHONY: update-dependencies
update-dependencies:
	$(GO) get go@latest
	# Remove patch level of GO version
	@sed -i.bak -E 's/(go [0-9]+\.[0-9]+)\.[0-9]+/\1/' go.mod
	@rm -f go.mod.bak
	$(GO) get -t -u $(PACKAGES)
	$(GO) mod tidy

	# Adding lines to changelog
	@sed -i.bak -e 's/\(## \[$(VERSION)\]\)/$(CHANGELOG_LINES)\1/' $(CHANGELOG)
	@rm -f $(CHANGELOG).bak

.PHONY: update
update: clean update-dependencies test

.PHONY: clean
clean:
	$(GO) clean
	rm -rf $(BUILD_DIR) $(DIST_DIR)
