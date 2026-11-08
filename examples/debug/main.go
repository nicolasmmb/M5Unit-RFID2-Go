//go:build esp32

// Step-by-step debug of the unit. Each step prints PASS/FAIL and the raw value.
package main

import (
	"time"

	"machine"

	rfid2 "github.com/nicolasmmb/M5Unit-RFID2-Go"
)

const hexDigits = "0123456789abcdef"

func hex2(v byte) { print(string(hexDigits[v>>4]), string(hexDigits[v&0x0f])) }

func bits(v byte) {
	// ComIrqReg: bit0 Timer, 1 Err, 2 LoAlert, 3 HiAlert, 4 Idle, 5 Rx, 6 Tx, 7 Set1.
	names := [8]string{"Timer", "Err", "LoAlert", "HiAlert", "Idle", "Rx", "Tx", "Set1"}
	for i := 0; i < 8; i++ {
		if v&(1<<uint(i)) != 0 {
			print(" ", names[i])
		}
	}
}

// crcB is the ISO/IEC 14443-B CRC: poly 0x1021 reflected, init 0xFFFF, and
// the result is complemented. Type A uses crcA instead; using the wrong one
// makes the frame invalid and the card silently ignores it.
func crcB(b []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, v := range b {
		crc ^= uint16(v)
		for i := 0; i < 8; i++ {
			if crc&1 != 0 {
				crc = crc>>1 ^ 0x8408
			} else {
				crc >>= 1
			}
		}
	}
	return ^crc
}

func main() {
	time.Sleep(1200 * time.Millisecond)

	i2c := machine.I2C1
	if err := i2c.Configure(machine.I2CConfig{
		SDA: machine.GPIO32, SCL: machine.GPIO33, Frequency: 100 * machine.KHz,
	}); err != nil {
		println("i2c:", err.Error())
		return
	}

	// STEP 1: the unit answers on the bus.
	var rx [1]byte
	if i2c.Tx(0x28, nil, rx[:]) != nil {
		println("STEP 1 FAIL: nothing at 0x28")
		return
	}
	println("STEP 1 PASS: device at 0x28")

	// STEP 2: reset, then dump the register file. An MFRC522 clone must show
	// sensible reset values (TxControl 0x80, Mode 0x3F, TxMode/RxMode 0x00).
	_ = i2c.Tx(0x28, []byte{0x01, 0x0F}, nil)
	time.Sleep(60 * time.Millisecond)
	print("STEP 2 reset registers:")
	read := func(reg byte) (byte, bool) {
		var b [1]byte
		if i2c.Tx(0x28, []byte{reg}, nil) != nil {
			return 0, false
		}
		if i2c.Tx(0x28, nil, b[:]) != nil {
			return 0, false
		}
		return b[0], true
	}
	for r := byte(0x00); r <= 0x0F; r++ {
		if v, ok := read(r); ok {
			print(" ")
			hex2(v)
		} else {
			print(" --")
		}
	}
	print(" | ")
	for r := byte(0x11); r <= 0x1A; r++ {
		if v, ok := read(r); ok {
			print(" ")
			hex2(v)
		}
	}
	print(" | ")
	for r := byte(0x21); r <= 0x2D; r++ {
		if v, ok := read(r); ok {
			print(" ")
			hex2(v)
		}
	}
	println()

	d := rfid2.New(i2c)
	if err := d.Init(); err != nil {
		println("STEP 2b FAIL init:", err.Error())
		return
	}
	print("STEP 2b PASS: version ")
	if v, err := d.Version(); err == nil {
		hex2(v)
	}
	println()

	// Widen the timer window: 4000 counts at 40 kHz = 100 ms, and give the
	// driver 150 ms. An emulated card can answer later than 25 ms.
	_ = d.SetRegister(0x2C, 0x0F) // TReloadH
	_ = d.SetRegister(0x2D, 0xA0) // TReloadL
	d.SetTimeout(150 * time.Millisecond)
	println("window widened to ~100 ms")

	// STEP 3: does the internal timer expire? It proves the 13.56 MHz
	// oscillator runs, which is a prerequisite for RF.
	println("STEP 3: timer / oscillator")
	t0 := time.Now()
	_, err := d.Transceive([]byte{0x26}, 7, 2)
	dt := time.Since(t0)
	c, _ := d.Register(0x04)
	print("  elapsed ms: ", int64(dt/time.Millisecond), " ComIrq:")
	bits(c)
	println()
	if dt > 10*time.Millisecond && dt < 80*time.Millisecond {
		println("  STEP 3 PASS: timer fired near 25 ms (oscillator alive)")
	} else {
		println("  STEP 3 FAIL: timer did not expire as expected, err:", err)
	}

	// STEP 4: antenna.
	tc, _ := d.Register(0x14)
	print("STEP 4 antenna: TxControlReg ")
	hex2(tc)
	if tc&0x03 == 0x03 {
		println(" PASS")
	} else {
		println(" FAIL (RF bits off)")
	}

	// STEP 5: transmission.
	_, _ = d.Transceive([]byte{0x26}, 7, 2)
	c, _ = d.Register(0x04)
	print("STEP 5 transmit: ComIrq:")
	bits(c)
	println()
	if c&0x40 != 0 {
		println("  STEP 5 PASS: TxIRq set (frame was sent)")
	} else {
		println("  STEP 5 FAIL: no TxIRq")
	}

	// STEP 6: receiver, with a card in the field.
	println("STEP 6: put the card on the coil now, 5 s of attempts")
	got := 0
	for i := 0; i < 10; i++ {
		card, err := d.Select()
		c, _ = d.Register(0x04)
		e, _ := d.Register(0x06)
		l, _ := d.Register(0x0A)
		print("  try ", i, " ComIrq:")
		bits(c)
		print(" ErrorReg ")
		hex2(e)
		println(" FIFO ", l)
		if err == nil {
			got++
			print("  CARD UID:")
			for _, b := range card.UIDBytes() {
				print(" ")
				hex2(b)
			}
			println(" SAK:", card.SAK)
			_ = d.Halt()
		}
		time.Sleep(400 * time.Millisecond)
	}
	if got > 0 {
		println("STEP 6 PASS: card answered", got, "times")
	} else {
		println("STEP 6 FAIL: no RxIRq (type A)")
	}

	// STEP 7: ISO14443-B probe with CRC_B (REQB, AFI 0x00, PARAM 0x00).
	println("STEP 7: type B probe (REQB + CRC_B), keep the card on the coil")
	reqb := []byte{0x05, 0x00, 0x00}
	crcb := crcB(reqb)
	reqb = append(reqb, byte(crcb), byte(crcb>>8))
	print("  REQB frame:")
	for _, b := range reqb {
		print(" ")
		hex2(b)
	}
	println()
	ok := 0
	for i := 0; i < 10; i++ {
		resp, err := d.Transceive(reqb, 0, 14)
		irq, _ := d.Register(0x04)
		e, _ := d.Register(0x06)
		print("  try ", i, " ComIrq:")
		bits(irq)
		print(" ErrorReg ")
		hex2(e)
		if len(resp) > 0 {
			print(" resp[", len(resp), "]:")
			for _, b := range resp {
				print(" ")
				hex2(b)
			}
			ok++
		}
		if err != nil {
			print(" ", err.Error())
		}
		println()
		time.Sleep(400 * time.Millisecond)
	}
	if ok > 0 {
		println("STEP 7 PASS: a type B card answered", ok, "times")
	} else {
		println("STEP 7 FAIL: no type B answer")
	}
}
