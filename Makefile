
.PHONY: run build clean logs vendor tidy deps

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

