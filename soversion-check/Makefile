PROJECT = soversion-check
MELANGE_CONTEXTDIR ?= /tmp/melange-context/$(PROJECT)
MELANGE_INSTALL_PATH = $(MELANGE_CONTEXTDIR)/usr/bin

.PHONY: build test clean melange-install

build:
	go build -o soversion-check

test:
	go test -v ./...

clean:
	rm -f soversion-check

melange-install: build
	mkdir -p $(MELANGE_INSTALL_PATH)
	install -Dm755 soversion-check $(MELANGE_INSTALL_PATH)/soversion-check