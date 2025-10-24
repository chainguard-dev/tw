PROJECT = no-docs-check
MELANGE_CONTEXTDIR ?= /tmp/melange-context/$(PROJECT)
MELANGE_INSTALL_PATH = $(MELANGE_CONTEXTDIR)/usr/bin

.PHONY: build test clean melange-install

build:
	go build -o no-docs-check

test:
	go test -v ./...

clean:
	rm -f no-docs-check

melange-install: build
	mkdir -p $(MELANGE_INSTALL_PATH)
	install -Dm755 no-docs-check $(MELANGE_INSTALL_PATH)/no-docs-check