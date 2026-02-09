BINARY := git-sonic
CMD := ./cmd/git-sonic
IMAGE_NAME ?= ghcr.io/mimelyc/git-sonic
IMAGE_TAG ?= latest
PLATFORMS ?= linux/amd64,linux/arm64

.PHONY: test fmt build run lint clean docker-build docker-run docker-push docker-buildx docker-buildx-push

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

# Single-arch build (local)
docker-build:
	docker build -t $(IMAGE_NAME):$(IMAGE_TAG) .

docker-run:
	docker run --rm --env-file .env -p 8080:8080 $(IMAGE_NAME):$(IMAGE_TAG)

docker-push:
	docker push $(IMAGE_NAME):$(IMAGE_TAG)

# Multi-arch build using buildx
docker-buildx-setup:
	docker buildx create --name git-sonic-builder --use --bootstrap 2>/dev/null || docker buildx use git-sonic-builder

docker-buildx: docker-buildx-setup
	docker buildx build --platform $(PLATFORMS) -t $(IMAGE_NAME):$(IMAGE_TAG) .

docker-buildx-push: docker-buildx-setup
	docker buildx build --platform $(PLATFORMS) -t $(IMAGE_NAME):$(IMAGE_TAG) --push .

docker-buildx-load: docker-buildx-setup
	docker buildx build --platform linux/$(shell go env GOARCH) -t $(IMAGE_NAME):$(IMAGE_TAG) --load .
