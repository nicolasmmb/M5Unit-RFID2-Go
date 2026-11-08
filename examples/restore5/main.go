//go:build esp32

// Puts block 5 back to all zeros (its content before the value-block test) and
// prints the read-back. The card may need to be lifted off the coil first so it
// powers up in a clean state.
package main

import (
	"time"

	"machine"

	rfid2 "github.com/nicolasmmb/M5Unit-RFID2-Go"
)

const block = 5

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
	println("=== RESTORE BLOCK 5 TO ZEROS ===")

	for attempt := 1; attempt <= 30; attempt++ {
		card, err := d.Select()
		if err != nil {
			if attempt%5 == 0 {
				println("waiting for a card:", err.Error())
			}
			time.Sleep(200 * time.Millisecond)
			continue
		}
		key := [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
		var uid4 [4]byte
		copy(uid4[:], card.UIDBytes())
		if err := d.Authenticate(block, rfid2.KeyA, key, uid4); err != nil {
			println("auth:", err.Error())
			time.Sleep(200 * time.Millisecond)
			continue
		}
		before, _ := d.ReadBlock(block)
		print("before:")
		for _, b := range before {
			print(" ", b)
		}
		println()

		var zeros [16]byte
		if err := d.WriteBlock(block, zeros); err != nil {
			println("write:", err.Error())
			time.Sleep(200 * time.Millisecond)
			continue
		}
		after, err := d.ReadBlock(block)
		print("after:")
		for _, b := range after {
			print(" ", b)
		}
		println()
		if err == nil && after == zeros {
			println("RESULT: block 5 is zeroed (original content) - PASS")
		} else {
			println("RESULT: check the read-back above")
		}
		_ = d.StopCrypto1()
		_ = d.Halt()
		return
	}
	println("RESULT: could not reach the card, lift it and place it again")
}
