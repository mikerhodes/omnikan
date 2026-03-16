.PHONY: build run rundemo

build:
	go build -o omnikan ./cmd/omnikan

run: build
	./omnikan

rundemo: build
	./omnikan -project "Demo Kanban"
