//go:build esp32

// Measures Select, Authenticate and ReadBlock on the device, in microseconds.
// Prints one line per sample and an average every 10 samples.
package main

import (
	"time"

	"machine"

	rfid2 "github.com/nicolasmmb/M5Unit-RFID2-Go"
)

const (
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
	println("=== RFID2 TIMING (400 kHz) ===")
	tone(2, 50) // ready

	key := [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	var uid4 [4]byte
	var n int
	var sumSel, sumAuth, sumRead int64
	waits := 0

	for {
		t0 := time.Now()
		card, err := d.Select()
		sel := time.Since(t0)
		if err != nil {
			waits++
			if waits%20 == 0 {
				println("waiting for a card")
			}
			time.Sleep(150 * time.Millisecond)
			continue
		}
		copy(uid4[:], card.UIDBytes())

		t0 = time.Now()
		authErr := d.Authenticate(4, rfid2.KeyA, key, uid4)
		auth := time.Since(t0)

		t0 = time.Now()
		_, readErr := d.ReadBlock(4)
		rd := time.Since(t0)

		print("select_us=", int64(sel/time.Microsecond),
			" auth_us=", int64(auth/time.Microsecond),
			" read_us=", int64(rd/time.Microsecond))
		if authErr != nil || readErr != nil {
			print(" err")
		}
		println()

		if authErr == nil && readErr == nil {
			n++
			sumSel += int64(sel / time.Microsecond)
			sumAuth += int64(auth / time.Microsecond)
			sumRead += int64(rd / time.Microsecond)
			if n%10 == 0 {
				println("AVG over", n, ": select_us=", sumSel/int64(n),
					" auth_us=", sumAuth/int64(n), " read_us=", sumRead/int64(n))
				tone(1, 40)
			}
		}
		_ = d.StopCrypto1()
		_ = d.Halt()
		time.Sleep(200 * time.Millisecond)
	}
}
