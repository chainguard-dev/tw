PROJECT = crane-check
MELANGE_CONTEXTDIR ?= /tmp/melange-context/$(PROJECT)
MELANGE_INSTALL_PATH = $(MELANGE_CONTEXTDIR)/usr/bin

.PHONY: build tidy vet test clean melange-install

build:
	go build -o crane-check

test:
	go test -v ./...

tidy:
	go mod tidy

vet:
	go vet ./...

clean:
	rm -f crane-check

melange-install: build
	mkdir -p $(MELANGE_INSTALL_PATH)
	install -Dm755 crane-check $(MELANGE_INSTALL_PATH)/crane-check
