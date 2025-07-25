PROJECT = apkq
MELANGE_CONTEXTDIR ?= /tmp/melange-context/$(PROJECT)
MELANGE_INSTALL_PATH = $(MELANGE_CONTEXTDIR)/usr

.PHONY: build test melange-install

build:
	go build -ldflags="-s -w" -o $(PROJECT)

test:
	go test -v ./...

melange-install: build
	mkdir -p $(MELANGE_INSTALL_PATH)/bin
	cp $(PROJECT) $(MELANGE_INSTALL_PATH)/bin/
