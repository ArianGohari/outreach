WAILS := $(shell go env GOPATH)/bin/wails
ifeq ($(shell go env GOOS),darwin)
export CGO_CFLAGS := -mmacosx-version-min=13.0
export CGO_LDFLAGS := -mmacosx-version-min=13.0
endif

.PHONY: setup dev test build-mac build-windows
setup:
	go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.0
	cd frontend && npm ci

dev:
	$(WAILS) dev

test:
	cd frontend && npm run build && npm test
	go test -race ./...
	go vet ./...

build-mac: test
	$(WAILS) build -platform darwin/arm64

build-windows: test
	$(WAILS) build -platform windows/amd64
