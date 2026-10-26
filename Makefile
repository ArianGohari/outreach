WAILS := $(shell go env GOPATH)/bin/wails
ifeq ($(shell go env GOOS),darwin)
export CGO_CFLAGS := -mmacosx-version-min=13.0
export CGO_LDFLAGS := -mmacosx-version-min=13.0
endif

.PHONY: setup dev test test-native build-mac build-windows
setup:
	go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.0
	cd frontend && npm ci

dev:
	$(WAILS) dev

test:
	cd frontend && npm run build && npm test
	go test -race -tags integration ./...
	go vet -tags integration ./...

test-native: test
	OUTREACH_OS_TESTS=1 go test -tags integration -run TestOperatingSystemCredentialRoundTrip .
	node scripts/native-e2e.mjs

build-mac: test
	$(WAILS) build -platform darwin/arm64

build-windows: test
	$(WAILS) build -platform windows/amd64
