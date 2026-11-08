//go:build esp32

// A/B test of the receiver gain at ONE fixed card position.
//
// Alternates 18 dB and 48 dB, six times each, 2 s per block, and prints the
// average success rate of each. Alternating cancels card drift: if the gain
// matters, the 48 dB blocks beat the 18 dB blocks by a clear margin.
//
// Sound: 1 beep = measuring 18 dB, 2 beeps = measuring 48 dB,
//
//	6 rounds (about 30 s), then 3 long beeps = done.
package main

import (
	"time"

	"machine"

	rfid2 "github.com/nicolasmmb/M5Unit-RFID2-Go"
)

const (
	buzzerPin = machine.GPIO2
	blockTime = 2 * time.Second
	rounds    = 1000 // runs until the power goes off
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

// measure runs Select attempts for blockTime and returns the success rate.
func measure(d *rfid2.Device, g rfid2.Gain) int {
	_ = d.SetAntennaGain(g)
	ok, attempts := 0, 0
	start := time.Now()
	for time.Since(start) < blockTime {
		if _, err := d.Select(); err == nil {
			ok++
			_ = d.Halt()
		}
		attempts++
		time.Sleep(15 * time.Millisecond)
	}
	if attempts == 0 {
		return 0
	}
	return ok * 100 / attempts
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

	println("=== GAIN A/B (18 dB vs 48 dB, same position) ===")
	tone(4, 60) // ready

	// Each pair is measured at (almost) the same position, so a change of
	// height hits both numbers together: that is what makes them comparable.
	better := 0
	for r := 1; r <= rounds; r++ {
		low := measure(d, rfid2.Gain18dB)
		high := measure(d, rfid2.Gain48dB)
		if high > low {
			better++
		}
		print("pair ", r, ": 18dB=", low, "% 48dB=", high, "%")
		if high > low {
			print("  <- 48 better")
		} else if low > high {
			print("  <- 18 better")
		}
		println("   pairs_with_48_better=", better, "/", r)
		tone(1, 30)
	}
}
