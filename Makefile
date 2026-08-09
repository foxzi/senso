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

.PHONY: build test vet fmt install version
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
