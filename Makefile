BINARY := agy-statusline
INSTALL_DIR ?= $(HOME)/.local/bin

.PHONY: build install test clean

build:
	go build -trimpath -ldflags="-s -w" -o $(BINARY) .

install: build
	mkdir -p $(INSTALL_DIR)
	cp $(BINARY) $(INSTALL_DIR)/$(BINARY)
	chmod +x $(INSTALL_DIR)/$(BINARY)

test:
	go test -v ./...

clean:
	rm -f $(BINARY)
