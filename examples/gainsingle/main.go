//go:build esp32

// Measures ONE gain, continuously, and prints the success rate every 2 s.
//
// Change `gain` and reflash to test the next value. Nothing alternates: the
// operator keeps the card at one position and the log fills with samples.
package main

import (
	"time"

	"machine"

	rfid2 "github.com/nicolasmmb/M5Unit-RFID2-Go"
)

const (
	gain      = rfid2.Gain18dB // A/B run: 18
	buzzerPin = machine.GPIO2
	window    = 2 * time.Second
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
	if err := d.SetAntennaGain(gain); err != nil {
		println("gain:", err.Error())
		return
	}
	back, _ := d.GetAntennaGain()
	print("MEASURING gain=")
	println(back.String(), back.DB(), "dB")
	tone(2, 60) // ready: measure now

	ok, attempts := 0, 0
	start := time.Now()
	last := time.Now()
	for {
		if _, err := d.Select(); err == nil {
			ok++
			_ = d.Halt()
		}
		attempts++

		if time.Since(last) >= window {
			pct := 0
			if attempts > 0 {
				pct = ok * 100 / attempts
			}
			print("["+back.String()+"] ", pct, "% ok=", ok, "/", attempts,
				" elapsed_ms=", int64(time.Since(start)/time.Millisecond))
			println()
			ok, attempts = 0, 0
			last = time.Now()
		}
		time.Sleep(15 * time.Millisecond)
	}
}
