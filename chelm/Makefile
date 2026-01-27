PROJECT = chelm
MELANGE_CONTEXTDIR ?= /tmp/melange-context/$(PROJECT)
MELANGE_INSTALL_PATH = $(MELANGE_CONTEXTDIR)/usr/bin

.PHONY: build test clean melange-install

build:
	go build -o chelm .

test:
	go test -v ./...

clean:
	rm -f chelm

melange-install: build
	mkdir -p $(MELANGE_INSTALL_PATH)
	install -Dm755 chelm $(MELANGE_INSTALL_PATH)/chelm
