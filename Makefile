.PHONY: build test clean

build:
	go build -o sophon .

test:
	go test ./...

clean:
	rm -f sophon
