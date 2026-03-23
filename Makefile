BIN := tcp-over-kafka

.PHONY: test build image clean

test:
	docker build -t $(BIN):test .

build:
	docker build -t $(BIN):latest .

image: build

clean:
	rm -f $(BIN)
