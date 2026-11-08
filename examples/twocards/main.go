//go:build esp32

// Enumerates every card in the field with SelectAll (bit-level anticollision +
// HALT of each card), cycling so the output can be read at any time.
package main

import (
	"time"

	"machine"

	rfid2 "github.com/nicolasmmb/M5Unit-RFID2-Go"
)

const hexDigit = "0123456789abcdef"

func hex2(b byte) { print(string(hexDigit[b>>4]), string(hexDigit[b&0x0f])) }

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

	println("=== TWO CARDS (TinyGo, bit-level anticollision) ===")
	for round := 1; ; round++ {
		cards, err := d.SelectAll(4)
		if err != nil {
			print("round ", round, " err: ")
			println(err.Error())
		} else {
			print("round ", round, " distinct=", len(cards))
			for _, c := range cards {
				print(" [")
				for _, b := range c.UIDBytes() {
					hex2(b)
				}
				print("]")
			}
			println()
		}
		time.Sleep(700 * time.Millisecond)
	}
}
