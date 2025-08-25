.PHONY: build test

HASH := \#
ARCH ?= $(shell uname -m)
ifeq (${ARCH}, arm64)
	ARCH = aarch64
endif

PROJECT_DIRS := $(patsubst ./%,%,$(shell find . -maxdepth 1 -type d -not -path "." -not -path "./.*" -not -path "./tools" -not -path "./packages"))

DIR_TESTS := $(addprefix test-, $(PROJECT_DIRS))

MELANGE ?= $(shell which melange)
KEY ?= local-melange.rsa
REPO ?= $(shell pwd)/packages
OUT_DIR ?= $(shell pwd)/packages

WOLFI_REPO ?= https://packages.wolfi.dev/os
WOLFI_KEY ?= https://packages.wolfi.dev/os/wolfi-signing.rsa.pub

MELANGE_OPTS += --arch ${ARCH}
MELANGE_OPTS += --keyring-append ${KEY}.pub
MELANGE_OPTS += --repository-append ${REPO}
MELANGE_OPTS += -k ${WOLFI_KEY}
MELANGE_OPTS += -r ${WOLFI_REPO}
MELANGE_OPTS += --source-dir ./

MELANGE_BUILD_OPTS += --signing-key ${KEY}
MELANGE_BUILD_OPTS += --cache-dir $(HOME)/go/pkg/mod
MELANGE_BUILD_OPTS += --out-dir ${OUT_DIR}

MELANGE_TEST_OPTS += --test-package-append wolfi-base

${KEY}:
	${MELANGE} keygen ${KEY}

build: $(KEY)
	$(MELANGE) build --runner docker melange.yaml $(MELANGE_OPTS) $(MELANGE_BUILD_OPTS)

test-%:
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

test: $(KEY) $(DIR_TESTS)
	$(MELANGE) test --runner docker melange.yaml $(MELANGE_OPTS) $(MELANGE_TEST_OPTS)

clean:
	rm -rf $(OUT_DIR)