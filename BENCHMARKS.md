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

## Device timing (measured, examples/timing)

M5StickC Plus v1 + Unit RFID2 + MIFARE Classic 1K, key A auth on block 4, CRC
checked on the answer:

| Operation | 100 kHz | 400 kHz |
|---|---:|---:|
| `Select` (REQA + anticollision + SELECT) | 28 524 µs | **11 104 µs** |
| `Authenticate` | 4 705 µs | **3 112 µs** |
| `ReadBlock` (18 bytes + CRC) | 9 145 µs | **5 113 µs** |
| full cycle | ~42 ms | **~19,3 ms** (~50 cycles/s) |

Averages over 40–50 samples at 400 kHz; the `Select` samples ranged
10 347–14 900 µs, the outliers being collision retries.

After the I2C round-trip reduction below: `Select` 10 347 µs, `Auth` 3 112 µs,
`Read` 4 928 µs.

### Reducing the I2C round-trips (v0.6.5)

Three changes: the FIFO flush is a plain `0x80` write (bits 6:0 are read-only,
nothing to preserve), `StartSend` goes in the same write as the framing bits,
and `ControlReg` (RxLastBits) is only read when the anticollision needs it.

| Operation | Before | After | Gain |
|---|---:|---:|---:|
| `Select` | 11 104 µs | 10 347 µs | **−6,8 %** |
| `Authenticate` | 3 112 µs | 3 112 µs | 0 (no RMW on that path) |
| `ReadBlock` | 5 113 µs | 4 928 µs | −3,6 % |

It is a small win, not the 13 % I estimated: a register read at 400 kHz costs
about 80 µs (write of the address + 1-byte read), not 250 µs. The remaining cost
is dominated by the ~6 register **writes** per transceive and by the three
transceives of `Select`; cutting that means caching register state, which is
where the subtle bugs live, so it is not worth it now.

Findings:

- **The unit does work at 400 kHz** (the first attempt only failed because the
  card was not on the coil), and it is 1,5–2,5× faster than 100 kHz. 400 kHz is
  the recommended setting.
- Most of the time is I2C register round-trips: one transceive needs about eight
  register reads/writes, and `Select` does three transceives.
- The three operations together cost ~19 ms, so a firmware can read a card
  about 50 times per second.

## Reliability at 400 kHz (measured)

`examples/soak`: 500 cycles of Select + Authenticate + ReadBlock with a MIFARE
Classic, I2C at 400 kHz, counting every error type:

```
ok=500  crc=0  proto=0  timeout=0  bus=0  other=0
error_rate_ppm=0   avg_cycle_us=22455   (~44 cycles/s)
```

Zero errors in 500 cycles. Zero out of 500 does not prove infinity: the upper
bound at 95 % confidence is about 6 000 ppm, so a longer soak would tighten it.

### Why 400 kHz is still worth a caveat

The chip allows 400 kHz; the **Grove cable** is the real limit.

| Risk | Effect at 400 kHz |
|---|---|
| 10 kΩ pull-ups on the module | rise time ~7 µs against a 1,25 µs bit — tight margin |
| cable capacitance (50–100 pF, 20 cm) | worse rise time |
| long cable, damaged cable, PaHub in series | adds capacitance → intermittent NACKs |

What protects the caller: the driver never returns corrupted data.

| Failure | Result |
|---|---|
| answer corrupted | `ErrCRC` (CRC_A verified in software) |
| frame or BCC wrong | `ErrProtocol` |
| bus NACK / no ACK | `ErrNotConnected` |

So a marginal bus degrades into **errors**, never into a wrong UID or block.

### Recommendation

| Environment | Speed |
|---|---|
| short Grove cable, no hub | **400 kHz** (measured: 0 errors, ~44 cycles/s) |
| long cable, hub, electrically noisy | **100 kHz** |
| maximum robustness | 100 kHz plus retry on `errors.Is` |

If `ErrNotConnected` starts appearing in the field, the first action is dropping
to 100 kHz: it is the classic symptom of insufficient pull-up, not a dead chip.

## What is still missing

| Item | Why it matters |
|---|---|
| Real card timing | REQA/anticol/select and read timings on the unit |
| CRC on real frames | The HALT vector `50 00 57 CD` is checked, the rest needs a card |
| 7-byte UID path | Different anticollision cascade |
