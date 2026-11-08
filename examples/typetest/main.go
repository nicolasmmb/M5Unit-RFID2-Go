//go:build esp32

// Prints the card type, for the parity check against the official
// PICC_GetTypeName. Waits for a card, so timing does not matter.
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

	println("=== TYPE PARITY (TinyGo) ===")
	var card rfid2.Card
	for {
		c, err := d.Select()
		if err == nil {
			card = c
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	print("uid=")
	for _, b := range card.UIDBytes() {
		hex2(b)
	}
	println()
	print("sak=")
	hex2(card.SAK)
	println()
	print("type_code=")
	println(int(card.Type()))
	print("type_name=")
	println(card.Type().String())
	println("=== END ===")
}
