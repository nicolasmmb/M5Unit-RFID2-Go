//go:build esp32

// Diagnostic firmware: shows what the chip reports while a card is near.
package main

import (
	"time"

	"machine"

	rfid2 "github.com/nicolasmmb/M5Unit-RFID2-Go"
)

const hexDigits = "0123456789abcdef"

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
	d.SetTimeout(30 * time.Millisecond)

	// Experiment: maximum receiver gain (48 dB) and the carrier bit cleared.
	_ = d.SetRegister(0x26, 0x87) // RxGainReg
	_ = d.SetRegister(0x14, 0x03) // TxControlReg: RF on, Tx1CW cleared
	tc, _ := d.Register(0x14)
	rx, _ := d.Register(0x26)
	println("diag start: TxControl", tc, "RxGain", rx)

	for i := 0; ; i++ {
		tc, _ := d.Register(0x14)
		// REQA with 7 bits.
		resp, err := d.Transceive([]byte{0x26}, 7, 8)
		e, _ := d.Register(0x06)
		c, _ := d.Register(0x04)
		l, _ := d.Register(0x0A)
		print("try", i, "TxControl", tc, "-> ")
		if err != nil {
			print(err.Error(), " ")
		}
		if len(resp) > 0 {
			print("resp[", len(resp), "]:")
			hex(resp)
			print(" ")
		}
		println("| ErrorReg", e, "ComIrq", c, "FIFOlevel", l)

		// ISO14443-B REQB, in case the card is type B.
		resp, err = d.Transceive([]byte{0x05, 0x00, 0x08, 0x71}, 0, 4)
		if err == nil && len(resp) > 0 {
			print("REQB resp:")
			hex(resp)
			println()
		} else if i%5 == 0 {
			println("REQB:", err)
		}
		time.Sleep(400 * time.Millisecond)
	}
}
