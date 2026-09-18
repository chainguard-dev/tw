PROJECT = daemon-check-output
MELANGE_CONTEXTDIR ?= /tmp/melange-context/$(PROJECT)
MELANGE_INSTALL_PATH = $(MELANGE_CONTEXTDIR)/usr/bin

.PHONY: build test clean melange-install

build:
	go build -o $(PROJECT)

test:
	go test -v ./...

clean:
	rm -f $(PROJECT)

melange-install: build
	mkdir -p $(MELANGE_INSTALL_PATH)
	install -Dm755 $(PROJECT) $(MELANGE_INSTALL_PATH)/$(PROJECT)
