//go:build esp32

// Reliability soak: N cycles of Select + Authenticate + ReadBlock at 400 kHz,
// counting successes and every error type. The CRC on the answer and the CRC of
// the frames are what turn a bus glitch into an error instead of wrong data.
package main

import (
	"errors"
	"time"

	"machine"

	rfid2 "github.com/nicolasmmb/M5Unit-RFID2-Go"
)

const (
	buzzerPin = machine.GPIO2
	cycles    = 500
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
	println("=== SOAK 400 kHz,", cycles, "cycles ===")
	tone(2, 50)

	key := [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	var uid4 [4]byte
	var ok, eCRC, eProto, eTimeout, eNotConn, eOther int
	start := time.Now()

	for i := 0; i < cycles; i++ {
		card, err := d.Select()
		if err != nil {
			switch {
			case errors.Is(err, rfid2.ErrCRC):
				eCRC++
			case errors.Is(err, rfid2.ErrProtocol):
				eProto++
			case errors.Is(err, rfid2.ErrTimeout):
				eTimeout++
			case errors.Is(err, rfid2.ErrNotConnected):
				eNotConn++
			default:
				eOther++
			}
			time.Sleep(50 * time.Millisecond)
			continue
		}
		copy(uid4[:], card.UIDBytes())

		if err := d.Authenticate(4, rfid2.KeyA, key, uid4); err != nil {
			if errors.Is(err, rfid2.ErrCRC) {
				eCRC++
			} else if errors.Is(err, rfid2.ErrAuth) {
				eProto++
			} else {
				eOther++
			}
			continue
		}
		if _, err := d.ReadBlock(4); err != nil {
			if errors.Is(err, rfid2.ErrCRC) {
				eCRC++
			} else {
				eOther++
			}
			continue
		}
		ok++
		_ = d.StopCrypto1()
		_ = d.Halt()

		if (i+1)%50 == 0 {
			print("at ", i+1, " ok=", ok, " crc=", eCRC, " proto=", eProto,
				" timeout=", eTimeout, " bus=", eNotConn, " other=", eOther)
			println()
			tone(1, 30)
		}
	}

	el := time.Since(start)
	println("------------------------------------------")
	println("ok=", ok, " crc=", eCRC, " proto=", eProto, " timeout=", eTimeout,
		" bus=", eNotConn, " other=", eOther)
	if ok > 0 {
		println("error_rate_ppm=", (cycles-ok)*1000000/cycles,
			" avg_cycle_us=", int64(el/time.Microsecond)/int64(cycles))
	}
	tone(3, 200)
}
