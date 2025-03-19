PROJECT = sht
MELANGE_CONTEXTDIR ?= /tmp/melange-context/$(PROJECT)
MELANGE_INSTALL_PATH = $(MELANGE_CONTEXTDIR)/usr

.PHONY: build melange-install

build:
	@rm -rf bin
	@mkdir -p bin
	# Build the runner
	go test -c -o bin/shtr
	# Build the client
	go build -o bin/shtc
	install -Dm755 sht bin/sht

melange-install: build
	echo $@
	mkdir -p $(MELANGE_INSTALL_PATH)
	cp -r bin $(MELANGE_INSTALL_PATH)
