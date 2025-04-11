GITCOMMIT := $(shell git rev-parse --short=7 HEAD 2>/dev/null)

.PHONY: precommit
precommit: clean format lint test compile

.PHONY: format
format:
	goimports -w -local github.com/digitalocean-labs/goldfish .
ifneq ($(shell which npx),)
	npx prettier --print-width 120 --bracket-same-line --write "app/*.(js|css|html)"
endif

.PHONY: lint
lint:
	golangci-lint run --build-tags prod

.PHONY: clean
clean:
	rm -rf target

target:
	mkdir target

.PHONY: test
test:
	go test -v -tags prod ./cmd/...

.PHONY: compile
compile: target
	go build -tags prod -ldflags "-s -w -X main.version=${GITCOMMIT}" -o target/ ./cmd/...

.PHONY: compile-dev
compile-dev: target
	go build -tags dev -ldflags "-s -w -X main.version=${GITCOMMIT}" -o target/ ./cmd/...

.PHONY: run
run: compile
	./target/goldfish

.PHONY: dev
dev: compile-dev
	./target/goldfish

.PHONY: bundle
bundle:
	gzip -k target/goldfish

.PHONY: local-redis
local-redis:
	docker run --rm -e 'ALLOW_EMPTY_PASSWORD=yes' -p "127.0.0.1:6379:6379" -it bitnami/redis:7.4.1

.PHONY: docker-build
docker-build:
ifdef IMAGE_BASE
	docker build --pull --platform=linux/amd64 --build-arg GITCOMMIT=${GITCOMMIT} --tag ${IMAGE_BASE}:${GITCOMMIT} --tag ${IMAGE_BASE}:latest .
else
	$(error "Please provide a IMAGE_BASE.")
endif

.PHONY: docker-push
docker-push:
ifdef IMAGE_BASE
	docker push ${IMAGE_BASE}:${GITCOMMIT}
	docker push ${IMAGE_BASE}:latest
else
	$(error "Please provide a IMAGE_BASE.")
endif

.PHONY: docker-run
docker-run:
ifdef IMAGE_BASE
	docker run --rm \
		-e 'PID_FILE=skip' \
		-e 'BACKEND_STORE=redis' \
		-e REDIS_ADDR \
		-e REDIS_USER \
		-e REDIS_PASS \
		-e REDIS_TLS \
		-p "127.0.0.1:3000:3000" \
		-it ${IMAGE_BASE}:${GITCOMMIT}
else
	$(error "Please provide a IMAGE_BASE.")
endif
