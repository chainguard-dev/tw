PROJECT = gosh
MELANGE_CONTEXTDIR ?= /tmp/melange-context/$(PROJECT)
MELANGE_INSTALL_PATH = $(MELANGE_CONTEXTDIR)/usr

.PHONY: build melange-install

build:
	@rm -rf bin
	@mkdir -p bin
	# Build the runner
	go test -c -o bin/goshr
	# Build the client
	go build -o bin/gt
	install -Dm755 gosh bin/gosh

test:
	go test -v ./internal/...

melange-install: build
	echo $@
	mkdir -p $(MELANGE_INSTALL_PATH)
	cp -r bin $(MELANGE_INSTALL_PATH)
