BINARY := git-sonic
CMD := ./cmd/git-sonic
IMAGE_NAME ?= git-sonic
IMAGE_TAG ?= latest

.PHONY: test fmt build run lint clean docker-build docker-run docker-push

test:
	go test ./...

fmt:
	gofmt -w cmd pkg tests

build:
	mkdir -p bin
	go build -o bin/$(BINARY) $(CMD)

run:
	go run $(CMD)

lint:
	go vet ./...

clean:
	rm -rf bin/

docker-build:
	docker build -t $(IMAGE_NAME):$(IMAGE_TAG) .

docker-run:
	docker run --rm --env-file .env -p 8080:8080 $(IMAGE_NAME):$(IMAGE_TAG)

docker-push:
	docker push $(IMAGE_NAME):$(IMAGE_TAG)
