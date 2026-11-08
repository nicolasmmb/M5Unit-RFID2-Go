//go:build esp32

// PARITY HARNESS — TinyGo side. Prints the same keys as the C++ harness so the
// two logs can be diffed. Waits for a card before printing the card block.
package main

import (
	"time"

	"machine"

	rfid2 "github.com/nicolasmmb/M5Unit-RFID2-Go"
)

const hexDigits = "0123456789abcdef"

func hex2(b byte) { print(string(hexDigits[b>>4]), string(hexDigits[b&0x0f])) }

func hexLine(key string, b []byte) {
	print(key, "=")
	for _, v := range b {
		hex2(v)
	}
	println()
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

	d := rfid2.New(i2c)
	if err := d.Init(); err != nil {
		println("init:", err.Error())
		return
	}

	println("=== PARITY HARNESS (TinyGo) ===")
	v, _ := d.Version()
	print("version=")
	hex2(v)
	println()

	tc, _ := d.Register(0x14)
	print("txcontrol=")
	hex2(tc)
	println()

	// RFCfgReg holds the antenna gain in bits 6:4, like PCD_GetAntennaGain.
	g, _ := d.Register(0x26)
	print("antenna_gain=")
	hex2(g & 0x70)
	println()

	// Software CRC_A of the frame 30 04: must match the chip in the C++ run.
	crc := crcA([]byte{0x30, 0x04})
	print("crc_3004=")
	hex2(byte(crc))
	hex2(byte(crc >> 8))
	println()

	// Wait for a card so the run does not depend on timing.
	var card rfid2.Card
	for {
		c, err := d.Select()
		if err == nil {
			card = c
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	print("reqa_atqa=")
	hex2(card.ATQA[0])
	hex2(card.ATQA[1])
	println()
	hexLine("select_uid", card.UIDBytes())
	print("select_sak=")
	hex2(card.SAK)
	println()

	print("type=")
	println(card.Type().String())

	key := [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	var uid4 [4]byte
	copy(uid4[:], card.UIDBytes())

	for _, blk := range []byte{0, 4, 8} {
		err := d.Authenticate(blk, rfid2.KeyA, key, uid4)
		print("auth", blk, "=")
		if err != nil {
			println("fail")
			continue
		}
		println("ok")
		data, err := d.ReadBlock(blk)
		if err != nil {
			println("read", blk, "=FAIL")
			continue
		}
		hexLine("read"+itoa(blk), data[:])
	}

	if err := d.StopCrypto1(); err == nil {
		println("stopcrypto=ok")
	} else {
		println("stopcrypto=fail")
	}
	if err := d.Halt(); err == nil {
		println("halt=ok")
	} else {
		println("halt=fail")
	}
	println("=== END ===")
}

func itoa(v byte) string {
	if v == 0 {
		return "0"
	}
	var b [3]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = '0' + v%10
		v /= 10
	}
	return string(b[i:])
}

// crcA copy so the harness prints the same value the library uses.
func crcA(b []byte) uint16 {
	crc := uint16(0x6363)
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
	return crc
}
