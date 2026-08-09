.PHONY: build test test-race clean

build:
	mkdir -p bin
	go build -o bin/purpc ./cmd/purpc
	go build -o bin/purpcmd-teamserver ./cmd/teamserver

test:
	go test ./...

test-race:
	go test -race ./pkg/teamapi ./server/callback ./server/implant ./server/listener ./server/lua ./teamserver/...

clean:
	rm -f bin/purpc bin/purpcmd-teamserver
