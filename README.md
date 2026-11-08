# M5Unit-RFID2-Go

[![test](https://github.com/nicolasmmb/M5Unit-RFID2-Go/actions/workflows/test.yml/badge.svg)](https://github.com/nicolasmmb/M5Unit-RFID2-Go/actions/workflows/test.yml)

Pure-Go driver for the **M5Stack Unit RFID2** (SKU **U031-B**): a 13.56 MHz
ISO/IEC 14443-A reader built on the **WS1850S**, which is register compatible
with the MFRC522. It replaces the RC522 of the first Unit RFID with the WS1850S
and keeps the same functions. No C, C++, CGo or Arduino.

Board agnostic: the core depends only on `Tx(addr, w, r)`, so `machine.I2C`
satisfies it on any chip and a fake chip makes the whole driver testable on the
host.

## Features

- `Init` with soft reset, version check, 40.8 kHz timer and antenna on.
- `Select`: REQA, anticollision and select, returning UID, SAK and ATQA.
- `Authenticate` with MIFARE key A or B. **Crypto1 runs inside the chip**, so
  there is no cryptography in Go.
- `ReadBlock` and `WriteBlock` with CRC_A checked in both directions.
- `Halt`, `Antenna`, `Reset`, `Version`.
- ISO/IEC 14443-A CRC_A implemented in software (no dependence on the chip
  command).
- Zero heap allocation per operation. `Device` is 104 bytes.
- Host tests with a chip emulator (registers, FIFO, scripted card), allocation
  and footprint contracts, benchmarks, and a CI gate that compiles for RP2040
  and nRF52840.

## Hardware

| Parameter | Value |
|---|---|
| Unit | RFID2, SKU U031-B |
| Chip | WS1850S (MFRC522 compatible registers) |
| Frequency | 13.56 MHz |
| Interface | **I2C at 0x28** |
| Standards | ISO/IEC 14443 A/B |
| Cards | MIFARE Classic, NTAG |
| Range | < 20 mm |

I2C protocol (same as the C library): write `[register, value]`; read with
`[register]` followed by the bytes. No register address bit shifting.

## Install

```bash
go get github.com/nicolasmmb/M5Unit-RFID2-Go@v0.1.0
```

## Quick start

```go
i2c := machine.I2C1
_ = i2c.Configure(machine.I2CConfig{
    SDA: machine.GPIO32, SCL: machine.GPIO33, Frequency: 100 * machine.KHz,
})

d := rfid2.New(i2c)
if err := d.Init(); err != nil {
    println("rfid2:", err.Error())
    return
}

card, err := d.Select()
if err != nil {
    println("card:", err.Error())   // ErrNoCard / ErrTimeout / ErrCollision
    return
}
println("UID", card.UID, "SAK", card.SAK)

key := [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
if err := d.Authenticate(4, rfid2.KeyA, key, card.UID); err != nil {
    println("auth:", err.Error())
    return
}
blk, err := d.ReadBlock(4)
if err == nil {
    println("block 4:", blk)
}
_ = d.Halt()
```

## API

| Function | Description |
|---|---|
| `New(bus I2C) *Device` | Default address 0x28, call `Init` |
| `NewAt(bus, addr) (*Device, error)` | Other address, runs `Init` |
| `Init() error` | Reset, version check, timers, antenna on |
| `Version() (byte, error)` | Version register (0x91/0x92) |
| `Antenna(on bool) error` | RF field on/off |
| `Reset() error` | Stop the command, clear the FIFO |
| `Select() (Card, error)` | REQA + anticollision + select |
| `SelectContext(ctx)` | Same with cancellation |
| `Authenticate(block, keyType, key, uid) error` | MIFARE key A/B |
| `ReadBlock(block) ([16]byte, error)` | Reads one block |
| `WriteBlock(block, data) error` | Writes one block |
| `Halt() error` | HALT the card in the field |
| `SetTimeout(d)` | Wait limit of one transceive (default 50 ms) |

`Card` carries `UID [4]byte`, `SAK byte`, `ATQA [2]byte`.

Errors: `ErrNotConnected`, `ErrBadVersion`, `ErrNoCard`, `ErrCollision`,
`ErrAuth`, `ErrCRC`, `ErrProtocol`, `ErrTimeout`, `ErrFrame`, `ErrBadBlock`.
Compare with `errors.Is`.

## Behaviour notes

- **CRC_A is computed in Go** (poly 0x8408 reflected, init 0x6363). Read and
  write frames carry it, and the answer CRC is verified for reads.
- Reads request 18 bytes (16 data + 2 CRC); writes send 20 bytes
  (command + block + 16 data + 2 CRC) and expect a 4-bit ACK `0x0A`.
- Authentication failure is read from `ErrorReg` with the same mask as the C
  driver (`0x1B`: ProtocolErr, ParityErr, CollErr, BufferOvfl).
- Only cascade level 1 is implemented, which covers MIFARE Classic 1K/4K and
  NTAG with a 4-byte UID. 7-byte UIDs (double cascade) are not handled yet.
- The buffer for the FIFO write is separate from the frame buffer, because the
  frame is built in `d.tx` and the FIFO transaction needs its own register
  prefix. That bug (clobbering the first frame byte) was caught by the tests.

## Performance

See [BENCHMARKS.md](BENCHMARKS.md). Host, fake chip:

| Operation | ns/op | allocs/op |
|---|---:|---:|
| `crcA` (4 bytes) | 32,0 | 0 |
| `readFIFO` (18 bytes) | 17,1 | 0 |
| transceive sequence (no card wait) | 68,9 | 0 |

RAM: `Device` 104 bytes. Flash: `examples/read` 22.3 KB on `m5stick-c`.

## Validation

```bash
make check      # vet + race tests + contracts + example + agnostic
make read PORT=/dev/cu.usbserial-XXXX
```

`make agnostic` compiles `New` for `pico` and `arduino-nano33`; CI repeats it.

## Known gaps

| Gap | Effect | Next step |
|---|---|---|
| Not validated on hardware yet | Card exchange is unproven on the unit | Flash `examples/read` with a card |
| Only cascade level 1 | 7-byte and 10-byte UIDs (NTAG21x, some cards) unsupported | Add cascade 2/3 in v0.2.0 |
| No ISO14443-B | The unit supports it, the driver does not | Separate phase |
| No MIFARE value/ultralight commands | Only block read/write and auth | Add if a project needs them |
| `readFIFO` calls `FIFOLevel` first | One extra I2C transaction per answer | Cache the level when the length is known |

## References

- Unit RFID2 (U031-B): https://docs.m5stack.com/en/unit/Unit_RFID2
- WS1850S datasheet, MFRC522 datasheet (register map and command set)
- Official C driver: `M5-ProductExampleCodes/Unit/RFID/Arduino/RFID/MFRC522_I2C.cpp`
- ISO/IEC 14443-3: REQA, anticollision, select and CRC_A

## License

MIT — see [LICENSE](LICENSE).
