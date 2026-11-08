//go:build esp32

// Card detector with sound.
//
//	boot              two short beeps (ready)
//	card detected     one long beep + UID on the serial port
//	card removed      silent, ready for the next card
//
// The buzzer of the M5StickC Plus2 is passive and sits on GPIO2, so the LEDC
// generates the square wave. The RFID2 uses I2C1 on GPIO32/GPIO33; no conflict.
//
// If nothing ever beeps, the serial port shows the IRQ flags every 5 s: TxIRq
// alone means the frame went out and no card answered.
package main

import (
	"time"

	"machine"

	rfid2 "github.com/nicolasmmb/M5Unit-RFID2-Go"
)

const (
	buzzerPin  = machine.GPIO2
	buzzerFreq = 2700 // Hz, the resonant point of the passive buzzer
	beepReady  = 60   // ms
	beepCard   = 300  // ms
	diagPeriod = 5 * time.Second
	pollPeriod = 150 * time.Millisecond
	hexDigits  = "0123456789abcdef"
	showDiag   = true
)

var (
	buzzer   = machine.PWM0
	buzzerCh uint8
)

func buzzerInit() {
	_ = buzzer.Configure(machine.PWMConfig{Period: uint64(time.Second / buzzerFreq)})
	ch, err := buzzer.Channel(buzzerPin)
	if err != nil {
		println("buzzer:", err.Error())
		return
	}
	buzzerCh = ch
}

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
	time.Sleep(time.Second)

	buzzerInit()

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
		println("rfid2 init:", err.Error())
		tone(3, 400) // error: three long beeps
		return
	}
	if v, err := d.Version(); err == nil {
		println("rfid2 ready, version:", v)
	}
	tone(2, beepReady) // ready: two short beeps

	lastDiag := time.Now()
	lastTx := false

	for {
		card, err := d.Select()
		if err == nil {
			tone(1, beepCard) // card detected
			print("CARD UID:")
			for i, b := range card.UID {
				if i > 0 {
					print(" ")
				}
				print(string(hexDigits[b>>4]), string(hexDigits[b&0x0f]))
			}
			println(" SAK:", card.SAK)
			_ = d.Halt()
			// Wait for the card to leave, then re-arm silently.
			for {
				time.Sleep(300 * time.Millisecond)
				if _, err := d.Select(); err != nil {
					break
				}
				_ = d.Halt()
			}
			lastDiag = time.Now()
			continue
		}

		if showDiag && time.Since(lastDiag) >= diagPeriod {
			lastDiag = time.Now()
			c, _ := d.Register(0x04) // ComIrqReg
			l, _ := d.Register(0x0A) // FIFOLevelReg
			tx := c&0x40 != 0
			if tx != lastTx || tx {
				lastTx = tx
			}
			println("waiting | ComIrq", c, "FIFO", l, "TX", tx, "err", err.Error())
		}
		time.Sleep(pollPeriod)
	}
}
