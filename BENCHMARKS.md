# Benchmarks — M5Unit-RFID2-Go

## Method

- Host: Apple Silicon (darwin/arm64), Go 1.26.3, `-count=2 -benchtime=200ms`.
- Chip fake that answers without allocating.
- The card wait is excluded: these numbers measure the driver, not the RF.

## Results

| Benchmark | Scenario | ns/op | allocs/op |
|---|---|---:|---:|
| `CRC` | CRC_A of a 4-byte frame | 32,0 | 0 |
| `ReadFIFO` | FIFOLevel read + 18-byte FIFO read | 17,1 | 0 |
| `Transceive` | register setup + command + IRQ + error + FIFO | 68,9 | 0 |

A real card exchange adds the RF time (a few ms for REQA/anticol/select, about
5 ms for a block read) and the I2C transfers at 100 kHz.

## Footprint

| Type | Size | Budget in the test |
|---|---:|---:|
| `Device` | 104 B | ≤ 128 B |

`Device` = 22-byte frame buffer + 22-byte FIFO buffer + 22-byte read buffer +
2-byte register scratch + interface pointers + timeout.

## Flash (TinyGo, `m5stick-c`)

| Target | code | data | bss | flash | ram |
|---|---:|---:|---:|---:|---:|
| `examples/read` | 22 100 | 172 | 4 696 | 22 272 B | 4 868 B |

## Contracts checked

```bash
make alloc      # zero allocation and footprint
make agnostic   # compiles New for pico and arduino-nano33
```

- `Select`, `Authenticate`, `ReadBlock`, `WriteBlock`, `Halt`, `crcA`:
  **0 allocs/op**.
- `New` allocates the `Device` once.

## Hardware measurements (M5StickC Plus2 + Unit RFID2)

| Item | Result |
|---|---|
| `Init` on the real unit | OK (reset, timer, antenna, register write + read-back) |
| `Version()` | **0x15** (WS1850S batch; not 0x91/0x92) |
| REQA with no card | `ErrTimeout` after 50 ms, as expected |
| `Select` with a card | not measured yet |

## Hardware validation

| Item | Result |
|---|---|
| `Init` on the real unit | OK (register dump matches MFRC522 reset values) |
| `Version()` | 0x15 |
| `Select` | UID `0A EE 8D 02`, SAK 0x08, ATQA 0x04 0x00 |
| `Authenticate` key A `FF..FF` | OK on blocks 0, 4 and 8 |
| `ReadBlock` | block 0 shows the manufacturer block, block 4/8 read back |
| Cross-check | the official C++ `MFRC522_I2C` reads the same card |

## What is still missing

| Item | Why it matters |
|---|---|
| Real card timing | REQA/anticol/select and read timings on the unit |
| CRC on real frames | The HALT vector `50 00 57 CD` is checked, the rest needs a card |
| 7-byte UID path | Different anticollision cascade |
