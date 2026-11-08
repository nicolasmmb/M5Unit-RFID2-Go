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
`[register]` **followed by a STOP**, then a separate read transaction. The unit
does not accept a repeated start for the register read — this was found on real
hardware and is the reason every register read is two transactions.

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
- **`VersionReg` is not a reliable presence test.** The real unit reports
  **0x15**, not the 0x91/0x92 of an MFRC522. `Init` therefore validates the chip
  by writing `TModeReg` and reading it back.
- Cascade levels 1 to 3 are implemented: 4-byte UIDs (MIFARE Classic), 7-byte
  UIDs (MIFARE Ultralight, NTAG) and 10-byte UIDs. `Card.UID` holds up to 10
  bytes; use `card.UIDBytes()` or `card.UIDLength()`.
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

## Hardware results

Verified on an M5StickC Plus2 with a real Unit RFID2:

```
version: 21          <- VersionReg really reports 0x15 on this batch
card: rfid2: timeout <- Init ok, REQA sent, no card in the field
```

So `Init` (reset, timer, antenna, register write + read-back) and the REQA
transceive work on the unit. `Select`, `Authenticate` and the block commands
still need a card on the antenna to be confirmed.

Facts found on real hardware that changed the driver:

1. The register read needs a **STOP between the address write and the read**;
   a repeated start returns garbage. Every register and FIFO read now uses two
   `Tx` calls, like the C library.
2. `VersionReg` returns **0x15**, not 0x91/0x92, so `Init` checks the chip with
   a write/read-back instead.
3. The FIFO must be **flushed** (`FIFOLevel = 0x80`) before every frame, and
   `StartSend` goes **after** the command. Without the flush, a leftover byte
   corrupted the next frame (the FIFO showed level 1).
4. The register transactions need their **own 2-byte buffer**. They shared the
   frame buffer, so a register write inside a transceive overwrote the frame
   (the HALT frame arrived as `13 00 57 CD` instead of `50 00 57 CD`).

## Troubleshooting: no card answers

Measured on the unit with cards and with a Flipper Zero emulating a card:

| Register | Value | Meaning |
|---|---|---|
| `VersionReg` | 0x15 | chip is there (not 0x91/0x92, normal for this batch) |
| `TxControlReg` | 0x03 | RF bits on |
| `TModeReg` read-back | 0x8D | writes land |
| `ErrorReg` | 0x00 | no protocol, parity or collision error |
| `ComIrqReg` | 0x45 | **TxIRq** + LoAlert + TimerIRq: frame went out, timer expired |
| `FIFOLevelReg` | 0x00 | FIFO flushed correctly |

So the digital path is proven end to end: register access, command execution,
frame transmission and FIFO handling. `RxIRq` never fires, which means the
receiver never sees a card answer.

Open hypotheses, in order:

1. **Card or emulator is not 13.56 MHz ISO14443-A.** A Flipper in the `RFID`
   app emulates 125 kHz and can never answer. It must be `NFC` → `Emulate`.
2. **RF section of this unit.** TxIRq does not prove the coil radiates: the chip
   reports "frame sent" even with a dead antenna. A known-good MIFARE Classic
   1K card, or the same unit on another board, separates this from hypothesis 1.
3. **Range.** The emulator must touch the coil, flat and aligned; the unit is
   rated below 20 mm and an emulated card is weaker than a real one.

Tried without change: `RxGainReg` 0x87 (48 dB, read-back returns 0 on this
clone), `Tx1CW` cleared, ISO14443-B REQB, WUPA wake-up, 20 ms field settle.

### Parity with the official C driver

After a line-by-line reading of `MFRC522_I2C.cpp`, these were aligned:

| Item | Official | Was | Now |
|---|---|---|---|
| `TModeReg` | 0x80 | 0x8D | 0x80 |
| `TPrescalerReg` | 0xA9 (169) | 0x3E | 0xA9 |
| Reload | 0x03E8 (1000, 25 ms) | 30 (~15 ms) | 0x03E8 |
| Reset | wait for PowerDown to clear | fixed sleep | waits |
| Wait IRQ | `RxIRq \| IdleIRq` (0x30) | `RxIRq` only | 0x30 |
| Error mask | 0x13 (BufferOvfl, ParityErr, ProtocolErr) | included CRCErr | 0x13 |

With all of that the behaviour on the unit is unchanged: the frame goes out
(TxIRq), the FIFO is clean and no card answers.

### Cross-check with the official C++ library

The same unit was flashed with a PlatformIO sketch that uses M5's own
`MFRC522_I2C.cpp` (from `M5-ProductExampleCodes`), no LCD, only `Wire`:

```
== official MFRC522_I2C library ==
version   = 0x15
TxControl = 0x83
TMode     = 0x80
reset regs 00-0F: 00 20 80 00 14 00 00 21 00 00 00 08 10 00 A0 00
REQA status=1 ATQA=04 00
ComIrq=0x64 ErrorReg=0x00 FIFO=0
```

`status=1` is STATUS_OK, `ATQA 04 00` is a MIFARE Ultralight and
`ComIrq=0x64` carries **RxIRq**. So the unit and the card work, and the
remaining defect is in this Go driver's transceive path.

Places where the Go driver still differs from the official sequence and that
are the next suspects:

1. it writes `TxModeReg` and `RxModeReg` (the official leaves them alone);
2. it writes the FIFO flush and `BitFraming` as plain writes instead of the
   official read-modify-write (`PCD_SetRegisterBitMask`).

### Board check (examples/pmic)

The diagnostic firmware scans the internal bus:

```
scan: 0x51 0x68        <- AXP192 + MPU6886: this is a StickC **Plus v1**, not v2
axp192 reg0x12: 0x51   <- bit 6 (EXTEN, the Grove 5 V rail) is already enabled
grove scan: 0x28       <- RFID2 answers
rfid2 version: 21
```

So the Grove 5 V rail is on and the module is a StickC Plus v1 (AXP192, not the
AXP2101 of the Plus2). That removes the power hypothesis.

## Known gaps

| Gap | Effect | Next step |
|---|---|---|
| Card exchange not validated yet | `Select`/`ReadBlock` need a card that answers | See the troubleshooting table above |
| ~~Only cascade level 1~~ | Fixed in v0.2.0: cascade 2 and 3, so 7-byte and 10-byte UIDs work | Done |
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
