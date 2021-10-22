GO			?= go
GOOPTS		?=

pkgs	 	= ./...

BIN_DIR		?= ./bin
BUILD_DIR	?= ./build
DIST_DIR	?= ./dist
APPNAME		?= galera-health
VERSION		?= $(shell cat ./VERSION 2> /dev/null)
COMMIT		?= $(shell git rev-parse --verify HEAD)
DATE		:= $(shell date +%FT%T%z)

GOPATH		?= $(shell go env GOPATH)
GO_LDFLAGS	+= -X main.name=$(APPNAME)
GO_LDFLAGS	+= -X main.version=v$(VERSION)
GO_LDFLAGS	+= -X main.commit=$(COMMIT)
GO_LDFLAGS	+= -X main.date=$(DATE)
# strip debug info from binary
GO_LDFLAGS += -s -w

GO_LDFLAGS := -ldflags="$(GO_LDFLAGS)"

OS		:= $(if $(GOOS),$(GOOS),$(shell go env GOOS))
ARCH	:= $(if $(GOARCH),$(GOARCH),$(shell go env GOARCH))

.PHONY: all
all: test build

.PHONY: build
build:
	$(GO) build $(GO_LDFLAGS) -o $(BUILD_DIR)/$(APPNAME)-$(VERSION)-$(OS)-$(ARCH)/$(APPNAME) -v ./...

.PHONY: test
test:
	mkdir -p $(BUILD_DIR)
	$(GO) test -v -race -coverprofile=$(BUILD_DIR)/test-coverage.out $(pkgs)

.PHONY: clean
clean:
	$(GO) clean
	rm -rf $(BUILD_DIR) $(DIST_DIR)

.PHONY: compile
compile:
	# FreeBDS
	GOOS=freebsd GOARCH=amd64 $(GO) build -o $(BUILD_DIR)/$(APPNAME)-$(VERSION)-freebsd-amd64/$(APPNAME) -v ./...
	# MacOS
	GOOS=darwin GOARCH=amd64 $(GO) build -o $(BUILD_DIR)/$(APPNAME)-$(VERSION)-darwin-amd64/$(APPNAME) -v ./...
	# Linux
	GOOS=linux GOARCH=amd64 $(GO) build -o $(BUILD_DIR)/$(APPNAME)-$(VERSION)-linux-amd64/$(APPNAME)  -v ./...
	# Windows
	GOOS=windows GOARCH=amd64 $(GO) build -o $(BUILD_DIR)/$(APPNAME)-$(VERSION)-windows-amd64/$(APPNAME)  -v ./...

.PHONY: dist
dist: clean compile
	mkdir -p $(DIST_DIR)

	tar -C $(BUILD_DIR) -cvf $(BUILD_DIR)/$(APPNAME)-$(VERSION)-freebsd-amd64.tar $(APPNAME)-$(VERSION)-freebsd-amd64
	gzip $(BUILD_DIR)/$(APPNAME)-$(VERSION)-freebsd-amd64.tar
	mv $(BUILD_DIR)/$(APPNAME)-$(VERSION)-freebsd-amd64.tar.gz $(DIST_DIR)/$(APPNAME)-$(VERSION)-freebsd-amd64.tar.gz

	tar -C $(BUILD_DIR) -cvf $(BUILD_DIR)/$(APPNAME)-$(VERSION)-darwin-amd64.tar $(APPNAME)-$(VERSION)-darwin-amd64
	gzip $(BUILD_DIR)/$(APPNAME)-$(VERSION)-darwin-amd64.tar
	mv $(BUILD_DIR)/$(APPNAME)-$(VERSION)-darwin-amd64.tar.gz $(DIST_DIR)/$(APPNAME)-$(VERSION)-darwin-amd64.tar.gz

	tar -C $(BUILD_DIR) -cvf $(BUILD_DIR)/$(APPNAME)-$(VERSION)-linux-amd64.tar $(APPNAME)-$(VERSION)-linux-amd64
	gzip $(BUILD_DIR)/$(APPNAME)-$(VERSION)-linux-amd64.tar
	mv $(BUILD_DIR)/$(APPNAME)-$(VERSION)-linux-amd64.tar.gz $(DIST_DIR)/$(APPNAME)-$(VERSION)-linux-amd64.tar.gz

	tar -C $(BUILD_DIR) -cvf $(BUILD_DIR)/$(APPNAME)-$(VERSION)-windows-amd64.tar $(APPNAME)-$(VERSION)-windows-amd64
	gzip $(BUILD_DIR)/$(APPNAME)-$(VERSION)-windows-amd64.tar
	mv $(BUILD_DIR)/$(APPNAME)-$(VERSION)-windows-amd64.tar.gz $(DIST_DIR)/$(APPNAME)-$(VERSION)-windows-amd64.tar.gz
