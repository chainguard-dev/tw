PROJECT = tw
MELANGE_CONTEXTDIR ?= /tmp/melange-context/$(PROJECT)
MELANGE_INSTALL_PATH = $(MELANGE_CONTEXTDIR)/usr

.PHONY: build melange-install

build:
	mkdir -p bin
	go build -o bin/tw
	for cmd in `bin/tw list-multicalls`; do \
		ln -sf tw bin/$$cmd && echo "Created symlink for $$cmd"; \
	done
	go test -c -o bin/twt

test:
	go test -v ./pkg/...

melange-install: build
	echo $@
	mkdir -p $(MELANGE_INSTALL_PATH)
	cp -r bin $(MELANGE_INSTALL_PATH)
