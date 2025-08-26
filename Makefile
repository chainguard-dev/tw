.PHONY: build test

TOP_D := $(patsubst %/,%,$(dir $(abspath $(lastword $(MAKEFILE_LIST)))))
HASH := \#
ARCH ?= $(shell uname -m)
ifeq (${ARCH}, arm64)
	ARCH = aarch64
endif

PROJECT_DIRS := $(patsubst ./%,%,$(shell find . -maxdepth 1 -type d -not -path "." -not -path "./.*" -not -path "./tools" -not -path "./packages"))

DIR_TESTS := $(addprefix test-, $(PROJECT_DIRS))

MELANGE ?= $(shell which melange)
KEY ?= local-melange.rsa
REPO ?= $(TOP_D)/packages
OUT_DIR ?= $(TOP_D)/packages

BIN_TOOLS_D = $(TOP_D)/tools/bin

YAM_FILES := $(shell find * .github -name "*.yaml" -type f)

WOLFI_REPO ?= https://packages.wolfi.dev/os
WOLFI_KEY ?= https://packages.wolfi.dev/os/wolfi-signing.rsa.pub

MELANGE_OPTS += --debug
MELANGE_OPTS += --arch=${ARCH}
MELANGE_OPTS += --keyring-append=${KEY}.pub
MELANGE_OPTS += --repository-append=${REPO}
MELANGE_OPTS += --keyring-append=${WOLFI_KEY}
MELANGE_OPTS += --repository-append=${WOLFI_REPO}
MELANGE_OPTS += --source-dir=./

MELANGE_BUILD_OPTS += --signing-key=${KEY}
MELANGE_BUILD_OPTS += --out-dir=${OUT_DIR}

MELANGE_TEST_OPTS += --test-package-append=wolfi-base

${KEY}:
	${MELANGE} keygen ${KEY}

build: $(KEY)
	$(MELANGE) build --runner docker melange.yaml $(MELANGE_OPTS) $(MELANGE_BUILD_OPTS)

test: $(DIR_TESTS)
.PHONY: $(DIR_TESTS)
$(DIR_TESTS): test-%:
	@echo "Running test in $*"
	@$(MAKE) -C $* test

shell_shbangre := ^$(HASH)!(/usr/bin/env[[:space:]]+|/bin/)(sh|bash)([[:space:]]+.*)?$$
shell_scripts := $(shell git ls-files | \
	xargs awk 'FNR == 1 && $$0 ~ sb { print FILENAME }' "sb=$(shell_shbangre)")

.PHONY: list-shellfiles shellcheck
list-shellfiles:
	@for s in $(shell_scripts); do echo $$s; done
shellcheck:
	@rc=0; for s in $(shell_scripts); do \
	    echo "shellcheck $$s"; \
	    shellcheck "$$s" || rc=$$?; \
	done; exit $$rc

test-melange: $(KEY)
	$(MELANGE) test --runner=docker melange.yaml $(MELANGE_OPTS) $(MELANGE_TEST_OPTS)

.PHONY: lint
lint: yam-check shellcheck

.PHONY: yam-check yam
# yam-check shows changes it would make and exits 0 on no changes.
yam-check: $(BIN_TOOLS_D)/yam
	$(BIN_TOOLS_D)/yam --lint $(YAM_FILES)

# yam applies changes to the files you cannot trust its exit code
yam: $(BIN_TOOLS_D)/yam
	$(BIN_TOOLS_D)/yam $(YAM_FILES)

$(BIN_TOOLS_D)/yam:
	@mkdir -p $(BIN_TOOLS_D)
	GOBIN=$(BIN_TOOLS_D) go install github.com/chainguard-dev/yam@v0.2.29
