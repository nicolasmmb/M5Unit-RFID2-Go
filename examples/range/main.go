//go:build esp32

// Range meter: beeps when the card appears and when it is lost, so the operator
// can count how many 5 mm steps the reader still sees the card.
//
// Change `gain` below to compare the minimum and the maximum receiver gain.
package main

import (
	"time"

	"machine"

	rfid2 "github.com/nicolasmmb/M5Unit-RFID2-Go"
)

const (
	gain      = rfid2.Gain48dB // <- first run was Gain18dB
	buzzerPin = machine.GPIO2
	hexDigit  = "0123456789abcdef"
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
	got, _ := d.GetAntennaGain()
	print("gain=")
	println(got.String(), got.DB())
	tone(3, 40) // ready

	seen := false
	misses := 0
	var firstSeen, lastSeen time.Time
	step := 0

	for {
		card, err := d.Select()
		if err == nil {
			if !seen {
				seen = true
				step++
				firstSeen = time.Now()
				tone(1, 60) // FOUND
				print("step ", step, " FOUND uid=")
				for _, b := range card.UIDBytes() {
					print(string(hexDigit[b>>4]), string(hexDigit[b&0x0f]))
				}
				println()
			}
			lastSeen = time.Now()
			misses = 0
			_ = d.Halt()
		} else {
			if seen {
				misses++
				if misses >= 5 { // 5 tries in a row: lost
					seen = false
					tone(2, 120) // LOST: two clear beeps
					println("step ", step, " LOST after", int64(lastSeen.Sub(firstSeen)/time.Millisecond), "ms")
					step++
				}
			}
		}
		time.Sleep(60 * time.Millisecond)
	}
}
