TAGS := sqlite_fts5

# Версия берётся из git: ближайший тег плюс, если сборка не строго на теге,
# число коммитов и хеш; суффикс -dirty означает несохранённые изменения.
# Вне git-репозитория (например, в распакованном архиве) остаётся "dev",
# и тогда версию подставляет runtime/debug из встроенной информации о сборке.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

MODULE  := senso/internal/version
LDFLAGS := -X $(MODULE).version=$(VERSION) -X $(MODULE).commit=$(COMMIT) -X $(MODULE).date=$(DATE)

DIST ?= dist
GOARCH := $(shell go env GOARCH)

# Версия пакета: без ведущей "v", дефисы заменены на "~", потому что rpm
# не допускает дефис в поле Version (для тега v0.1.0 получается 0.1.0).
PKGVERSION := $(subst -,~,$(patsubst v%,%,$(VERSION)))

.PHONY: build test vet fmt install version clean package
build:
	go build -tags $(TAGS) -ldflags "$(LDFLAGS)" -o bin/senso .
test:
	go test -tags $(TAGS) ./...
vet:
	go vet -tags $(TAGS) ./...
fmt:
	gofmt -w .
install:
	go install -tags $(TAGS) -ldflags "$(LDFLAGS)" .
version:
	@echo $(VERSION)
clean:
	rm -rf bin $(DIST)
# Локальная сборка пакетов для текущей архитектуры.
# Требуется nfpm: go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest
package: build
	@command -v nfpm >/dev/null 2>&1 || { \
		echo "nfpm not found: go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest"; \
		exit 1; }
	mkdir -p $(DIST)
	VERSION=$(PKGVERSION) ARCH=$(GOARCH) nfpm package -f packaging/nfpm.yaml -p deb -t $(DIST)/
	VERSION=$(PKGVERSION) ARCH=$(GOARCH) nfpm package -f packaging/nfpm.yaml -p rpm -t $(DIST)/
