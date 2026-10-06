.PHONY: test run docker

test:
	go vet ./... && go test -race -count=1 ./...

run:
	go run ./cmd/ledgerd

docker:
	docker build -t ledger . && docker run --rm -p 8080:8080 ledger
