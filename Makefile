.PHONY: build run

build:
	go build -o omnikan ./cmd/omnikan

run: build
	./omnikan
