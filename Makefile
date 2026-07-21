.PHONY: check fix

check:
	test -z "$$(gofmt -l .)"
	go vet ./...
	go test -race ./...
	go build ./cmd/noema

fix:
	gofmt -w .
