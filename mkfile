build:V:
	go build -v || ( goimports -w . && go build -v )

lint:V:
	goimports -w .
	errcheck
	staticcheck

test:V:
	go test -v ./...
