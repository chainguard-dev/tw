PROJECT = package-type-check
MELANGE_CONTEXTDIR ?= /tmp/melange-context/$(PROJECT)
MELANGE_INSTALL_PATH = $(MELANGE_CONTEXTDIR)/usr/bin

.PHONY: build test clean melange-install

build:
	go build -o package-type-check

test:
	go test -v ./...

clean:
	rm -f package-type-check

melange-install: build
	mkdir -p $(MELANGE_INSTALL_PATH)
	install -Dm755 package-type-check $(MELANGE_INSTALL_PATH)/package-type-check