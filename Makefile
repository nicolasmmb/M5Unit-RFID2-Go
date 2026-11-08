TARGET ?= m5stick-c
PORT ?=
TINYGO ?= tinygo
COUNT ?= 8
BENCH ?= .
BENCHTIME ?= 1s

.PHONY: test vet fmt check bench bench-save bench-compare alloc footprint escape profile examples agnostic read

test:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

check: vet test alloc examples agnostic

bench:
	COUNT=$(COUNT) BENCH=$(BENCH) BENCHTIME=$(BENCHTIME) ./scripts/bench.sh run

bench-save:
	COUNT=$(COUNT) BENCH=$(BENCH) BENCHTIME=$(BENCHTIME) ./scripts/bench.sh save $(NAME)

bench-compare:
	./scripts/bench.sh compare $(FROM) $(TO)

alloc:
	./scripts/bench.sh alloc

footprint:
	go test -run TestFootprint -v

escape:
	./scripts/bench.sh escape

profile:
	./scripts/bench.sh profile

examples:
	$(TINYGO) build -target=$(TARGET) -size short -o /tmp/m5rfid2-read.bin ./examples/read

# Board agnosticism, no hardware required.
agnostic:
	@rm -rf /tmp/rfid-agn && mkdir -p /tmp/rfid-agn && cd /tmp/rfid-agn && \
	  MOD=$(shell awk '/^module /{print $$2}' go.mod) && \
	  printf 'module agn\n\ngo 1.26\n\nrequire %s v0.0.0\nreplace %s => %s\n' "$$MOD" "$$MOD" "$(CURDIR)" > go.mod && \
	  printf 'package main\n\nimport lib "%s"\n\nvar _ = lib.New\nvar _ = lib.Card{}\n\nfunc main() {}\n' "$$MOD" > main.go && \
	  go mod tidy >/dev/null && \
	  $(TINYGO) build -target=pico -o /tmp/rfid-agn/pico.bin . && \
	  $(TINYGO) build -target=arduino-nano33 -o /tmp/rfid-agn/nrf.bin . && \
	  echo "agnostic: pico + arduino-nano33 OK"

read:
	$(TINYGO) flash -target=$(TARGET) -port=$(PORT) -size short -monitor ./examples/read
