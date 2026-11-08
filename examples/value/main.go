//go:build esp32

// Value block test on block 5: write the value block format, run the four
// value commands, and restore the original content at the end.
package main

import (
	"time"

	"machine"

	rfid2 "github.com/nicolasmmb/M5Unit-RFID2-Go"
)

const (
	block     = 5
	buzzerPin = machine.GPIO2
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
	println("=== VALUE BLOCK TEST (block 5) ===")

	var card rfid2.Card
	for {
		c, err := d.Select()
		if err == nil {
			card = c
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	key := [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	var uid4 [4]byte
	copy(uid4[:], card.UIDBytes())
	if err := d.Authenticate(block, rfid2.KeyA, key, uid4); err != nil {
		println("auth:", err.Error())
		return
	}
	tone(1, 60) // card + auth ok

	original, err := d.ReadBlock(block)
	if err != nil {
		println("read original:", err.Error())
		return
	}
	print("original:")
	for _, b := range original {
		print(" ", b)
	}
	println()

	// Whatever happens below, block 5 goes back to its original content.
	defer func() {
		if err := d.WriteBlock(block, original); err != nil {
			println("RESTORE FAILED:", err.Error())
			return
		}
		back, err := d.ReadBlock(block)
		println("original restored:", err == nil && back == original)
	}()

	step := func(label string, fn func() error) bool {
		if err := fn(); err != nil {
			println(label, "FAIL", err.Error())
			return false
		}
		return true
	}

	// 1. Format the block as a value block with 100.
	if !step("write value 100", func() error { return d.WriteBlock(block, rfid2.FormatValue(100, block)) }) {
		return
	}
	data, err := d.ReadBlock(block)
	if err != nil {
		println("read:", err.Error())
		return
	}
	v, err := rfid2.ValueFromBlock(data, block)
	println("1 after write: value =", v, "err =", err)

	// 2. Increment by 10 and transfer.
	if !step("increment", func() error { return d.ValueIncrement(block, 10) }) ||
		!step("transfer", func() error { return d.ValueTransfer(block) }) {
		return
	}
	data, _ = d.ReadBlock(block)
	v, _ = rfid2.ValueFromBlock(data, block)
	println("2 after increment 10: value =", v)

	// 3. Decrement by 30 and transfer.
	if !step("decrement", func() error { return d.ValueDecrement(block, 30) }) ||
		!step("transfer", func() error { return d.ValueTransfer(block) }) {
		return
	}
	data, _ = d.ReadBlock(block)
	v, _ = rfid2.ValueFromBlock(data, block)
	println("3 after decrement 30: value =", v)

	// 4. Stage +50, then Restore to discard it, then transfer: value unchanged.
	if !step("increment 50 (pending)", func() error { return d.ValueIncrement(block, 50) }) ||
		!step("restore", func() error { return d.ValueRestore(block) }) ||
		!step("transfer", func() error { return d.ValueTransfer(block) }) {
		return
	}
	data, _ = d.ReadBlock(block)
	v, _ = rfid2.ValueFromBlock(data, block)
	println("4 after restore (pending +50 discarded): value =", v)
	if v == 80 {
		println("VERDICT: PASS (100 +10 -30 = 80, restore works)")
		tone(2, 120)
	} else {
		println("VERDICT: FAIL, expected 80")
	}

	tone(3, 100)
	println("=== END ===")
	_ = d.StopCrypto1()
	_ = d.Halt()
}
