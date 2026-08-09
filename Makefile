TAGS := sqlite_fts5

.PHONY: build test vet fmt install
build:
	go build -tags $(TAGS) -o bin/senso .
test:
	go test -tags $(TAGS) ./...
vet:
	go vet -tags $(TAGS) ./...
fmt:
	gofmt -w .
install:
	go install -tags $(TAGS) .
