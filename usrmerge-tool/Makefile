PROJECT = usrmerge-tool
MELANGE_CONTEXTDIR ?= /tmp/melange-context/$(PROJECT)
MELANGE_INSTALL_PATH = $(MELANGE_CONTEXTDIR)/usr/bin

.PHONY: build melange-install

build:
	@mkdir -p bin
	go build -o bin/$(PROJECT)

test:
	go test -v ./...

melange-install: build
	echo $@
	mkdir -p $(MELANGE_INSTALL_PATH)
	install -Dm755 bin/$(PROJECT) $(MELANGE_INSTALL_PATH)/$(PROJECT)
