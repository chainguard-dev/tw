PROJECT = tw-upstream-pytest
MELANGE_CONTEXTDIR ?= /tmp/melange-context/$(PROJECT)
MELANGE_INSTALL_PATH = $(MELANGE_CONTEXTDIR)/usr/bin
# The pytest plugin and helper scripts: plain files, not a Python package,
# so one copy serves every python3.X. The runner copies them into its
# scratch directory and puts that on PYTHONPATH.
LIB_INSTALL_PATH = $(MELANGE_CONTEXTDIR)/usr/libexec/tw/upstream-pytest
LIB_FILES = $(wildcard lib/*.py)

.PHONY: build test melange-install

build:
	chmod +x $(PROJECT)

# Needs a python3.X that can import pytest (tests/run.sh says so if none).
test:
	@./tests/run.sh

melange-install: build
	echo $@
	mkdir -p $(MELANGE_INSTALL_PATH) $(LIB_INSTALL_PATH)
	install -Dm755 $(PROJECT) $(MELANGE_INSTALL_PATH)/$(PROJECT)
	install -m644 $(LIB_FILES) $(LIB_INSTALL_PATH)/
