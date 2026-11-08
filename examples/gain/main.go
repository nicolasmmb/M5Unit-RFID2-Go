//go:build esp32

// Gain sweep at a FIXED card distance.
//
// For each gain (18..48 dB) it runs 1.5 s of Select attempts and prints the
// success rate. The operator only has to keep the card at one fixed distance:
// nothing moves during the sweep, so the numbers are comparable.
package main

import (
	"time"

	"machine"

	rfid2 "github.com/nicolasmmb/M5Unit-RFID2-Go"
)

const (
	buzzerPin = machine.GPIO2
	perGain   = 1500 * time.Millisecond
)

var (
	buzzer   = machine.PWM0
	buzzerCh uint8
	gains    = []rfid2.Gain{
		rfid2.Gain18dB, rfid2.Gain23dB, rfid2.Gain28dB, rfid2.Gain33dB,
		rfid2.Gain38dB, rfid2.Gain43dB, rfid2.Gain48dB,
	}
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

func main() {
	time.Sleep(1200 * time.Millisecond)

	_ = buzzer.Configure(machine.PWMConfig{Period: uint64(time.Second / 2700)})
	if ch, err := buzzer.Channel(buzzerPin); err == nil {
		buzzerCh = ch
	}

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

	println("=== GAIN SWEEP (fixed distance) ===")
	tone(3, 50)

	for round := 1; round <= 3; round++ {
		println("--- round", round)
		for _, g := range gains {
			if err := d.SetAntennaGain(g); err != nil {
				println("gain:", err.Error())
				return
			}
			back, _ := d.GetAntennaGain()

			attempts, ok := 0, 0
			start := time.Now()
			for time.Since(start) < perGain {
				if card, err := d.Select(); err == nil {
					ok++
					_ = d.Halt()
					_ = card
				}
				attempts++
				time.Sleep(15 * time.Millisecond)
			}
			pct := 0
			if attempts > 0 {
				pct = ok * 100 / attempts
			}
			print("gain=", back.DB(), "dB readback=", int(back), " ok=", ok, "/", attempts, " success=", pct, "%")
			println()
			tone(1, 35) // one beep per gain step
		}
	}
	println("=== END ===")
	tone(2, 250)
}
