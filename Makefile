.PHONY: build test

TOP_D := $(patsubst %/,%,$(dir $(abspath $(lastword $(MAKEFILE_LIST)))))
HASH := \#
ARCH ?= $(shell uname -m)
ifeq (${ARCH}, arm64)
	ARCH = aarch64
endif

PROJECT_DIRS := $(patsubst ./%,%,$(shell find . -maxdepth 1 -type d -not -path "." -not -path "./.*" -not -path "./tools" -not -path "./packages"))

PROJECT_TESTS := $(addprefix test-project/, $(PROJECT_DIRS))

MELANGE ?= $(shell which melange)
KEY ?= local-melange.rsa
REPO ?= $(TOP_D)/packages
OUT_DIR ?= $(TOP_D)/packages

TEST_DIR ?= $(TOP_D)/tests
TEST_OUT_DIR ?= $(TEST_DIR)/packages
TEST_PIPELINE_FILES := $(wildcard $(TEST_DIR)/*-test.yaml)

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

MELANGE_TEST_OPTS += --repository-append ${OUT_DIR}
MELANGE_TEST_OPTS += --repository-append ${TEST_OUT_DIR}
MELANGE_TEST_OPTS += --keyring-append ${KEY}.pub
MELANGE_TEST_OPTS += --arch ${ARCH}
MELANGE_TEST_OPTS += --pipeline-dirs ${TOP_D}/pipelines
MELANGE_TEST_OPTS += --repository-append ${WOLFI_REPO}
MELANGE_TEST_OPTS += --keyring-append ${WOLFI_KEY}
MELANGE_TEST_OPTS += --test-package-append wolfi-base

${KEY}:
	${MELANGE} keygen ${KEY}

build: $(KEY)
	$(MELANGE) build --runner docker melange.yaml $(MELANGE_OPTS) $(MELANGE_BUILD_OPTS)

test-projects: $(PROJECT_TESTS)
.PHONY: $(PROJECT_TESTS)
$(PROJECT_TESTS): test-project/%:
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

clean:
	rm -rf $(OUT_DIR) && rm -rf $(TEST_OUT_DIR)

# Test the main melange.yaml package
.PHONY: test-melange
test-melange: $(KEY)
	@echo "==> Testing melange.yaml package..."
	$(MELANGE) test --runner=docker melange.yaml $(MELANGE_OPTS) $(MELANGE_TEST_OPTS)

# Build pipeline test packages
.PHONY: build-pipeline-tests
build-pipeline-tests: $(KEY)
	@echo "==> Building pipeline test packages..."
	@rc=0; for test_file in $(TEST_PIPELINE_FILES); do \
		echo "Building $$test_file"; \
		$(MELANGE) build --runner docker $$test_file $(MELANGE_OPTS) --signing-key=${KEY} --pipeline-dir ${TOP_D}/pipelines --out-dir=${TEST_OUT_DIR} || rc=$$?; \
		if [ $$rc -ne 0 ]; then \
			echo "ERROR: Build failed for $$test_file" >&2; \
			exit $$rc; \
		fi; \
	done; exit $$rc

# Run pipeline validation tests
.PHONY: run-pipeline-tests
run-pipeline-tests: $(KEY)
	@echo "==> Running pipeline validation tests..."
	@rc=0; for test_file in $(TEST_PIPELINE_FILES); do \
		echo "Testing $$test_file"; \
		$(MELANGE) test --runner docker $$test_file $(MELANGE_OPTS) $(MELANGE_TEST_OPTS) || rc=$$?; \
		if [ $$rc -ne 0 ]; then \
			echo "ERROR: Tests failed for $$test_file" >&2; \
			exit $$rc; \
		fi; \
	done; exit $$rc

# Complete pipeline test suite (clean + build + test)
.PHONY: test-pipelines
test-pipelines:
	@echo "==> Running complete pipeline test suite..."
	$(MAKE) build-pipeline-tests
	$(MAKE) run-pipeline-tests

# Run all tests
.PHONY: test-all
test-all:
	@echo "==> Running all tests..."
	$(MAKE) test-melange
	$(MAKE) test-projects
	$(MAKE) test-pipelines

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
