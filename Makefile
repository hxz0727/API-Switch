.PHONY: build install run clean lint test docker

BINARY   = api-switch
OUTPUT   = ./$(BINARY)
SRC      = ./cmd/api-switch/
LDFLAGS  = -ldflags="-s -w"
# 禁止 Go 自动下载 toolchain（使用本地 Go 版本编译）
export GOTOOLCHAIN=local

build:
	go build $(LDFLAGS) -o $(OUTPUT) $(SRC)

install: build
	mkdir -p $(HOME)/.local/bin
	cp $(OUTPUT) $(HOME)/.local/bin/$(BINARY)
	@echo "Installed to $(HOME)/.local/bin/$(BINARY)"

run: build
	./$(BINARY) serve

clean:
	rm -f $(OUTPUT)

lint:
	golangci-lint run --timeout=3m

test:
	go vet ./...
	go test -v -count=1 -race ./...

tidy:
	go mod tidy

docker:
	docker build -t api-switch .

# 快捷命令
deepseek: build
	./$(BINARY) provider add deepseek --key $(key)

qwen: build
	./$(BINARY) provider add qwen --key $(key)

doctor: build
	./$(BINARY) doctor
