PROJECT = ver-check
MELANGE_CONTEXTDIR ?= /tmp/melange-context/$(PROJECT)
MELANGE_INSTALL_PATH = $(MELANGE_CONTEXTDIR)/usr/bin

.PHONY: build melange-install

build:
	chmod +x ver-check

test:

melange-install: build
	echo $@
	mkdir -p $(MELANGE_INSTALL_PATH)
	install -Dm755 $(PROJECT) $(MELANGE_INSTALL_PATH)/$(PROJECT)
