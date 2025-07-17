PROJECT = symlink-check
MELANGE_CONTEXTDIR ?= /tmp/melange-context/$(PROJECT)
MELANGE_INSTALL_PATH = $(MELANGE_CONTEXTDIR)/usr/bin

.PHONY: build test clean melange-install

build:
	go build -o symlink-check

test:
	go test -v ./...

clean:
	rm -f symlink-check

melange-install: build
	mkdir -p $(MELANGE_INSTALL_PATH)
	install -Dm755 symlink-check $(MELANGE_INSTALL_PATH)/symlink-check