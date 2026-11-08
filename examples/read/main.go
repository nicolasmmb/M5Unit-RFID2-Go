//go:build esp32

// Validation firmware for the Unit RFID2.
//
// It prints the version once, then every card that reaches the antenna: UID,
// SAK, ATQA, the result of a key A authentication on block 4 and the block
// contents. "waiting" is printed every 2 s while the field is empty.
package main

import (
	"time"

	"machine"

	rfid2 "github.com/nicolasmmb/M5Unit-RFID2-Go"
)

const hexDigits = "0123456789abcdef"

// The M5StickC Plus2 has a passive buzzer on GPIO2: it needs a square wave, so
// the LEDC (machine.PWM0) generates it. 2.7 kHz is the usual resonant point.
const buzzerPin = machine.GPIO2

var (
	buzzer   = machine.PWM0
	buzzerCh uint8
)

func buzzerInit() {
	_ = buzzer.Configure(machine.PWMConfig{Period: uint64(time.Second / 2700)})
	ch, err := buzzer.Channel(buzzerPin)
	if err != nil {
		println("buzzer:", err.Error())
		return
	}
	buzzerCh = ch
}

// tone plays n beeps of ms milliseconds with a pause between them.
func tone(n, ms int) {
	for i := 0; i < n; i++ {
		buzzer.Set(buzzerCh, buzzer.Top()/2)
		time.Sleep(time.Duration(ms) * time.Millisecond)
		buzzer.Set(buzzerCh, 0)
		if i < n-1 {
			time.Sleep(time.Duration(ms) * time.Millisecond)
		}
	}
}

func hex(b []byte) {
	for i, v := range b {
		if i > 0 {
			print(" ")
		}
		print(string(hexDigits[v>>4]), string(hexDigits[v&0x0f]))
	}
}

func main() {
	time.Sleep(time.Second)

	i2c := machine.I2C1
	if err := i2c.Configure(machine.I2CConfig{
		SDA:       machine.GPIO32,
		SCL:       machine.GPIO33,
		Frequency: 100 * machine.KHz,
	}); err != nil {
		println("i2c:", err.Error())
		return
	}

	buzzerInit()
	tone(2, 60) // ready: two short beeps

	d := rfid2.New(i2c)
	if err := d.Init(); err != nil {
		println("rfid2 init:", err.Error())
		return
	}
	if v, err := d.Version(); err == nil {
		println("rfid2 ready, version:", v)
	}

	// Diagnostics once, so the antenna state is visible.
	if v, err := d.Register(0x14); err == nil { // TxControlReg
		println("TxControlReg:", v, "(3 = antenna on)")
	}
	if v, err := d.Register(0x04); err == nil { // ComIrqReg
		println("ComIrqReg:", v)
	}

	key := [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	var lastWait time.Time
	diagDone := false

	for {
		card, err := d.Select()
		if err != nil {
			if time.Since(lastWait) >= 2*time.Second {
				lastWait = time.Now()
				println("waiting for a card:", err.Error())
				if !diagDone {
					diagDone = true
					// Only once, to see what the chip reports after a REQA.
					e, _ := d.Register(0x06) // ErrorReg
					c, _ := d.Register(0x04) // ComIrqReg
					l, _ := d.Register(0x0A) // FIFOLevelReg
					println("diag: ErrorReg", e, "ComIrqReg", c, "FIFOLevel", l)
				}
			}
			time.Sleep(150 * time.Millisecond)
			continue
		}

		tone(1, 250) // card detected: one long beep

		print("UID:")
		hex(card.UIDBytes())
		println()
		println("SAK:", card.SAK, "ATQA:", card.ATQA[0], card.ATQA[1])

		for _, blk := range []byte{0, 4, 8} {
			if err := d.Authenticate(blk, rfid2.KeyA, key, card.UID); err != nil {
				println("auth block", blk, ":", err.Error())
				continue
			}
			println("auth block", blk, ": ok")
			data, err := d.ReadBlock(blk)
			if err != nil {
				println("read block", blk, ":", err.Error())
				continue
			}
			print("  block", blk, ":")
			hex(data[:])
			println()
			tone(2, 40) // block read: two short beeps
		}

		_ = d.Halt()
		time.Sleep(1 * time.Second)
	}
}
