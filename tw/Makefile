PROJECT = tw
MELANGE_CONTEXTDIR ?= /tmp/melange-context/$(PROJECT)
MELANGE_INSTALL_PATH = $(MELANGE_CONTEXTDIR)/usr/bin

.PHONY: build build-tw build-twt melange-install-tw melange-install-twt

build: build-tw build-twt

build-tw:
	mkdir -p bin/tw
	go build -o bin/tw/tw
	for cmd in `bin/tw/tw list-multicalls`; do \
		ln -sf tw bin/tw/$$cmd && echo "Created symlink for $$cmd at bin/tw/$$cmd"; \
	done

build-twt:
	mkdir -p bin/twt
	go test -c -o bin/twt/twt

melange-install-tw: build-tw
	echo $@
	mkdir -p $(MELANGE_INSTALL_PATH)
	cp -r bin/tw/* $(MELANGE_INSTALL_PATH)

melange-install-twt: build-twt
	echo $@
	mkdir -p $(MELANGE_INSTALL_PATH)
	cp -r bin/twt/* $(MELANGE_INSTALL_PATH)
