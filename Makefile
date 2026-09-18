.PHONY: test race vet build cross run

test:
	go test ./...

race:
	go test -race ./...

vet:
	go vet ./...

build:
	go build -o h3-oneclick ./cmd/h3-oneclick

cross:
	mkdir -p dist
	GOOS=windows GOARCH=amd64 go build -o dist/h3-oneclick-windows-amd64.exe ./cmd/h3-oneclick
	GOOS=linux GOARCH=amd64 go build -o dist/h3-oneclick-linux-amd64 ./cmd/h3-oneclick
	GOOS=darwin GOARCH=arm64 go build -o dist/h3-oneclick-macos-arm64 ./cmd/h3-oneclick

run:
	go run ./cmd/h3-oneclick
