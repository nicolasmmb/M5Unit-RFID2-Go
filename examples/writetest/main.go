//go:build esp32

// Writes one data block and restores it, printing every step.
//
// SAFETY: only block 4 (first data block of sector 1). Never block 0
// (manufacturer) and never a sector trailer (3, 7, 11...): a wrong trailer can
// lock the card forever. The original content is read first and written back at
// the end.
package main

import (
	"time"

	"machine"

	rfid2 "github.com/nicolasmmb/M5Unit-RFID2-Go"
)

const (
	block    = 4
	hexDigit = "0123456789abcdef"
)

func hex2(b byte) { print(string(hexDigit[b>>4]), string(hexDigit[b&0x0f])) }

func hexLine(key string, b []byte) {
	print(key, "=")
	for _, v := range b {
		hex2(v)
	}
	println()
}

func same(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func main() {
	time.Sleep(1200 * time.Millisecond)

	i2c := machine.I2C1
	if err := i2c.Configure(machine.I2CConfig{
		SDA: machine.GPIO32, SCL: machine.GPIO33, Frequency: 100 * machine.KHz,
	}); err != nil {
		println("i2c:", err.Error())
		return
	}
	d := rfid2.New(i2c)
	if err := d.Init(); err != nil {
		println("init:", err.Error())
		return
	}

	println("=== WRITE TEST (TinyGo) block 4 ===")

	var card rfid2.Card
	for {
		c, err := d.Select()
		if err == nil {
			card = c
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	hexLine("uid", card.UIDBytes())

	key := [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	var uid4 [4]byte
	copy(uid4[:], card.UIDBytes())

	if err := d.Authenticate(block, rfid2.KeyA, key, uid4); err != nil {
		println("auth=fail", err.Error())
		return
	}
	println("auth=ok")

	original, err := d.ReadBlock(block)
	if err != nil {
		println("read_back_original=fail", err.Error())
		return
	}
	hexLine("original", original[:])

	// Pattern: "TINYGO-WRITE-OK!" (16 bytes).
	pattern := [16]byte{}
	copy(pattern[:], []byte("TINYGO-WRITE-OK!"))

	if err := d.WriteBlock(block, pattern); err != nil {
		println("write=fail", err.Error())
		c, _ := d.Register(0x04)
		e, _ := d.Register(0x06)
		l, _ := d.Register(0x0A)
		println("  diag: ComIrq", c, "ErrorReg", e, "FIFO", l)
		// Did the write land even without the ACK?
		if after, rerr := d.ReadBlock(block); rerr == nil {
			hexLine("after_failed_write", after[:])
		} else {
			println("  read after write:", rerr.Error())
		}
		return
	}
	println("write=ok")

	back, err := d.ReadBlock(block)
	if err != nil {
		println("read_back=fail", err.Error())
		return
	}
	hexLine("readback", back[:])
	if same(back[:], pattern[:]) {
		println("verify=PASS")
	} else {
		println("verify=FAIL")
	}

	// Restore the original content.
	if err := d.WriteBlock(block, original); err != nil {
		println("restore=fail", err.Error())
		return
	}
	after, err := d.ReadBlock(block)
	if err != nil {
		println("read_after_restore=fail", err.Error())
		return
	}
	hexLine("restored", after[:])
	if same(after[:], original[:]) {
		println("restore=PASS")
	} else {
		println("restore=FAIL")
	}

	_ = d.StopCrypto1()
	_ = d.Halt()
	println("=== END ===")
}
