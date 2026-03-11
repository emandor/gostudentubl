
.PHONY: run build clean logs vendor tidy deps docker-build docker-up docker-down docker-logs docker-restart docker-signal-run docker-deploy

run:
	@echo ">> Running gostudentubl..."
	@set -a; . ./.env; set +a; \
	go run ./cmd/gostudentubl

build:
	@echo ">> Building gostudentubl..."
	@set -a; . ./.env; set +a; \
	go build -o bin/gostudentubl ./cmd/gostudentubl

clean:
	@echo ">> Cleaning..."
	rm -rf bin
	rm -rf vendor
	rm -rf *.out

logs:
	@echo ">> Tailing logs..."
	@tail -f logs/app.log

vendor:
	@echo ">> Vendoring dependencies..."
	go mod tidy
	go mod vendor

tidy:
	@echo ">> Go mod tidy..."
	go mod tidy

deps:
	@echo ">> Downloading dependencies..."
	go mod download

docker-build:
	@echo ">> Building Docker image..."
	docker build -t gostudentubl:latest .

docker-up:
	@echo ">> Starting Docker deployment..."
	docker compose up -d --build

docker-down:
	@echo ">> Stopping Docker deployment..."
	docker compose down

docker-logs:
	@echo ">> Tailing Docker logs..."
	docker compose logs -f attendance-agent

docker-restart:
	@echo ">> Restarting Docker service..."
	docker compose restart attendance-agent

docker-signal-run:
	@echo ">> Triggering manual attendance run (SIGUSR1)..."
	docker compose kill -s SIGUSR1 attendance-agent

docker-deploy: docker-up
