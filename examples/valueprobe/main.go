//go:build esp32

// Probes the MIFARE value commands frame by frame, printing the raw answer of
// each variant, and restores block 5 at the end.
package main

import (
	"time"

	"machine"

	rfid2 "github.com/nicolasmmb/M5Unit-RFID2-Go"
)

const (
	block = 5
	hexD  = "0123456789abcdef"
)

func hex2(b byte) { print(string(hexD[b>>4]), string(hexD[b&0x0f])) }

func hexLine(key string, b []byte) {
	print(key, "=")
	for _, v := range b {
		hex2(v)
	}
	println()
}

func crcA(b []byte) uint16 {
	crc := uint16(0x6363)
	for _, v := range b {
		crc ^= uint16(v)
		for i := 0; i < 8; i++ {
			if crc&1 != 0 {
				crc = crc>>1 ^ 0x8408
			} else {
				crc >>= 1
			}
		}
	}
	return crc
}

// probe sends frame (CRC appended when withCRC) and prints what came back.
func probe(d *rfid2.Device, label string, frame []byte, withCRC bool, rxLen int) bool {
	f := append([]byte(nil), frame...)
	if withCRC {
		c := crcA(f)
		f = append(f, byte(c), byte(c>>8))
	}
	hexLine(label+" sent", f)
	resp, err := d.Transceive(f, 0, rxLen)
	irq, _ := d.Register(0x04)
	e, _ := d.Register(0x06)
	l, _ := d.Register(0x0A)
	print("  ComIrq=")
	hex2(irq)
	print(" ErrorReg=")
	hex2(e)
	println(" FIFO=", l)
	if len(resp) > 0 {
		hexLine("  resp", resp)
	}
	if err != nil {
		println("  err:", err.Error())
		return false
	}
	println("  OK")
	return true
}

func main() {
	time.Sleep(1200 * time.Millisecond)

	i2c := machine.I2C1
	if err := i2c.Configure(machine.I2CConfig{
		SDA: machine.GPIO32, SCL: machine.GPIO33, Frequency: 400 * machine.KHz,
	}); err != nil {
		println("i2c:", err.Error())
		return
	}
	d := rfid2.New(i2c)
	if err := d.Init(); err != nil {
		println("init:", err.Error())
		return
	}
	println("=== VALUE PROBE (block 5) ===")

	var card rfid2.Card
	for {
		c, err := d.Select()
		if err == nil {
			card = c
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	key := [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	var uid4 [4]byte
	copy(uid4[:], card.UIDBytes())
	if err := d.Authenticate(block, rfid2.KeyA, key, uid4); err != nil {
		println("auth:", err.Error())
		return
	}
	original, _ := d.ReadBlock(block)
	hexLine("original", original[:])

	// Make sure the block is a value block with 100.
	if err := d.WriteBlock(block, rfid2.FormatValue(100, block)); err != nil {
		println("write value block:", err.Error())
		return
	}
	data, _ := d.ReadBlock(block)
	hexLine("value block", data[:])

	// 1. Restore: 4 bytes, no operand. Simplest member of the family.
	println("--- variant A: Restore C2 05 + CRC")
	probe(d, "  C2_05", []byte{0xC2, block}, true, 1)

	// 2. Transfer: 4 bytes.
	println("--- variant B: Transfer B0 05 + CRC")
	probe(d, "  B0_05", []byte{0xB0, block}, true, 1)

	// 3. Increment in one frame: C1 + block + operand + CRC.
	println("--- variant C: Increment in one frame")
	inc := []byte{0xC1, block, 10, 0, 0, 0}
	probe(d, "  C1_one", inc, true, 1)

	// 4. Increment without CRC.
	println("--- variant D: Increment without CRC")
	probe(d, "  C1_nocrc", inc, false, 1)

	// 5. Increment in two phases: command, then operand.
	println("--- variant E: Increment two phases")
	probe(d, "  C1_phase1", []byte{0xC1, block}, true, 1)
	probe(d, "  operand", []byte{10, 0, 0, 0}, true, 1)

	// Restore the original content of block 5.
	if err := d.WriteBlock(block, original); err != nil {
		println("RESTORE FAILED:", err.Error())
		return
	}
	back, _ := d.ReadBlock(block)
	hexLine("restored", back[:])
	println("=== END ===")
	_ = d.StopCrypto1()
	_ = d.Halt()
}
