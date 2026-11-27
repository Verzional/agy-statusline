BINARY := agy-statusline
INSTALL_DIR ?= $(HOME)/.local/bin

.PHONY: build install test bench clean

build:
	go build -trimpath -ldflags="-s -w" -o $(BINARY) .

install: build
	mkdir -p $(INSTALL_DIR)
	cp $(BINARY) $(INSTALL_DIR)/$(BINARY)
	chmod +x $(INSTALL_DIR)/$(BINARY)

test:
	go vet ./...
	go test -race -v ./...

bench:
	go test -bench=. -benchmem ./...

clean:
	rm -f $(BINARY)
