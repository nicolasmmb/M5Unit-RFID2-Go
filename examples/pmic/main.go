//go:build esp32

// PMIC diagnostic + card test.
//
//  1. Scans the internal I2C bus (G21/G22) and prints every address found.
//  2. If the AXP2101 is there (0x34), dumps the status registers and the
//     rail-enable block (0x80..0x9F), which shows whether the 5 V rail that
//     feeds the Grove port is enabled.
//  3. Then runs the RFID2 and beeps on a detected card.
package main

import (
	"time"

	"machine"

	rfid2 "github.com/nicolasmmb/M5Unit-RFID2-Go"
)

const (
	pmicAddr  = 0x34
	buzzerPin = machine.GPIO2
	hexDigits = "0123456789abcdef"
)

var (
	buzzer   = machine.PWM0
	buzzerCh uint8
)

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

func hex2(v byte) {
	print(string(hexDigits[v>>4]), string(hexDigits[v&0x0f]))
}

func main() {
	time.Sleep(1200 * time.Millisecond)

	// Buzzer.
	_ = buzzer.Configure(machine.PWMConfig{Period: uint64(time.Second / 2700)})
	if ch, err := buzzer.Channel(buzzerPin); err == nil {
		buzzerCh = ch
	}
	tone(2, 50)

	// ---- internal bus: PMIC ----
	i2c0 := machine.I2C0
	if err := i2c0.Configure(machine.I2CConfig{
		SDA: machine.GPIO21, SCL: machine.GPIO22, Frequency: 100 * machine.KHz,
	}); err != nil {
		println("i2c0:", err.Error())
	} else {
		print("scan:")
		var rx [1]byte
		found := 0
		for a := uint16(0x08); a <= 0x77; a++ {
			if i2c0.Tx(a, nil, rx[:]) == nil {
				print(" 0x")
				hex2(byte(a))
				found++
			}
		}
		println(" (", found, "devices )")

		// AXP192 (0x51) at least: enable the external 5 V rail (EXTEN, reg
		// 0x12 bit 6). That rail feeds the Grove port, and it is off after
		// power-on. Without it the RFID2 chip answers on I2C but the antenna
		// driver has no supply, so no card can answer.
		if _, ok := func() (byte, bool) {
			var b [1]byte
			if i2c0.Tx(0x51, []byte{0x12}, b[:]) != nil {
				if i2c0.Tx(0x51, nil, b[:]) != nil {
					return 0, false
				}
			}
			return b[0], true
		}(); ok {
			var b [1]byte
			_ = i2c0.Tx(0x51, []byte{0x12}, b[:])
			if i2c0.Tx(0x51, nil, b[:]) != nil {
				println("axp192: no answer")
			} else {
				before := b[0]
				after := before | 0x40
				_ = i2c0.Tx(0x51, []byte{0x12, after}, nil)
				time.Sleep(50 * time.Millisecond)
				var c [1]byte
				_ = i2c0.Tx(0x51, []byte{0x12}, nil)
				_ = i2c0.Tx(0x51, nil, c[:])
				print("axp192 reg0x12: before 0x")
				hex2(before)
				print(" after 0x")
				hex2(c[0])
				println(" (bit6 = EXTEN / Grove 5 V)")
			}
		}

		// AXP2101 status and rail enables.
		read := func(reg byte) (byte, bool) {
			var b [1]byte
			if i2c0.Tx(pmicAddr, []byte{reg}, b[:]) != nil {
				if i2c0.Tx(pmicAddr, nil, b[:]) != nil {
					return 0, false
				}
			}
			return b[0], true
		}
		if v, ok := read(0x03); ok {
			print("pmic chip id: 0x")
			hex2(v)
			println()
			println("regs 0x00..0x07:")
			for r := byte(0x00); r <= 0x07; r++ {
				if v, ok := read(r); ok {
					print("  0x")
					hex2(r)
					print(" = 0x")
					hex2(v)
					println()
				}
			}
			println("rail enables 0x80..0x9F:")
			for r := byte(0x80); r <= 0x9F; r++ {
				v, ok := read(r)
				if !ok {
					continue
				}
				if r%4 == 0 {
					print("  0x")
					hex2(r)
					print(":")
				}
				print(" ")
				hex2(v)
				if r%4 == 3 {
					println()
				}
			}
		} else {
			println("pmic: no answer at 0x34")
		}
	}

	// ---- Grove bus: RFID2 ----
	i2c1 := machine.I2C1
	if err := i2c1.Configure(machine.I2CConfig{
		SDA: machine.GPIO32, SCL: machine.GPIO33, Frequency: 100 * machine.KHz,
	}); err != nil {
		println("i2c1:", err.Error())
		return
	}
	print("grove scan:")
	var rx [1]byte
	for a := uint16(0x08); a <= 0x77; a++ {
		if i2c1.Tx(a, nil, rx[:]) == nil {
			print(" 0x")
			hex2(byte(a))
		}
	}
	println()

	d := rfid2.New(i2c1)
	if err := d.Init(); err != nil {
		println("rfid2:", err.Error())
		return
	}
	if v, err := d.Version(); err == nil {
		println("rfid2 version:", v)
	}

	for {
		card, err := d.Select()
		if err == nil {
			tone(1, 300)
			print("CARD UID:")
			for _, b := range card.UIDBytes() {
				print(" ")
				hex2(b)
			}
			println(" SAK:", card.SAK)
			_ = d.Halt()
			time.Sleep(time.Second)
			continue
		}
		time.Sleep(200 * time.Millisecond)
	}
}
