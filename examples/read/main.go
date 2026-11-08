//go:build esp32

// Example: read the UID of a MIFARE card on the serial port.
package main

import (
	"time"

	"machine"

	rfid2 "github.com/nicolasmmb/M5Unit-RFID2-Go"
)

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

	d := rfid2.New(i2c)
	if err := d.Init(); err != nil {
		println("rfid2:", err.Error())
		if v, e := d.Version(); e == nil {
			println("version register:", v)
		}
		return
	}
	if v, err := d.Version(); err == nil {
		println("version:", v)
	}

	key := [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	for {
		card, err := d.Select()
		if err != nil {
			println("card:", err.Error())
			time.Sleep(300 * time.Millisecond)
			continue
		}
		const hex = "0123456789abcdef"
		print("UID:")
		for _, b := range card.UID {
			print(" ", string(hex[b>>4]), string(hex[b&0xf]))
		}
		println("SAK:", card.SAK)

		if err := d.Authenticate(4, rfid2.KeyA, key, card.UID); err != nil {
			println("auth:", err.Error())
		} else if blk, err := d.ReadBlock(4); err == nil {
			print("block 4:")
			for _, b := range blk {
				print(" ", b)
			}
			println()
		}
		_ = d.Halt()
		time.Sleep(500 * time.Millisecond)
	}
}
