.PHONY: build

ARCH ?= $(shell uname -m)
ifeq (${ARCH}, arm64)
	ARCH = aarch64
endif

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
MELANGE_OPTS += --out-dir ${OUT_DIR}

MELANGE_BUILD_OPTS += --signing-key ${KEY}
MELANGE_BUILD_OPTS += --cache-dir $(HOME)/go/pkg/mod

MELANGE_TEST_OPTS += --test-package-append wolfi-base

${KEY}:
	${MELANGE} keygen ${KEY}

build: $(KEY)
	$(MELANGE) build melange.yaml $(MELANGE_OPTS) $(MELANGE_BUILD_OPTS)

debug: $(KEY)
	$(MELANGE) build --interactive melange.yaml $(MELANGE_OPTS) $(MELANGE_BUILD_OPTS)

test: $(KEY)
	$(MELANGE) test melange.yaml $(MELANGE_OPTS) $(MELANGE_TEST_OPTS)
