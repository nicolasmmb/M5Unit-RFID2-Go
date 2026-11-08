//go:build esp32

// Is this card a "magic" card (UID writable)?
//
// Safe test: read block 0 and write the SAME 16 bytes back.
//
//	Gen2 magic  -> the card accepts the write (content stays identical)
//	normal card -> the card refuses block 0 (NAK / no answer)
//	Gen1a magic -> refuses a normal write; it needs the 0x40 backdoor first
//
// Because the content written is the content read, the UID and the BCC stay
// valid either way.
package main

import (
	"time"

	"machine"

	rfid2 "github.com/nicolasmmb/M5Unit-RFID2-Go"
)

const hexDigit = "0123456789abcdef"

func hex2(b byte) { print(string(hexDigit[b>>4]), string(hexDigit[b&0x0f])) }

func hexLine(key string, b []byte) {
	print(key, "=")
	for _, v := range b {
		hex2(v)
	}
	println()
}

// crcA is the ISO14443-A CRC, duplicated here because the library does not
// expose a raw block-0 write on purpose.
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

// rawWrite sends the two-phase MIFARE write without the library's block guard.
func rawWrite(d *rfid2.Device, block byte, data [16]byte) error {
	frame := []byte{0xA0, block}
	c := crcA(frame)
	frame = append(frame, byte(c), byte(c>>8))
	rx, err := d.Transceive(frame, 0, 1)
	if err != nil {
		return err
	}
	if rx[0]&0x0F != 0x0A {
		return errNack
	}

	frame = append([]byte(nil), data[:]...)
	c = crcA(frame)
	frame = append(frame, byte(c), byte(c>>8))
	rx, err = d.Transceive(frame, 0, 1)
	if err != nil {
		return err
	}
	if rx[0]&0x0F != 0x0A {
		return errNack
	}
	return nil
}

type errString string

func (e errString) Error() string { return string(e) }

var errNack = errString("nak")

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

	println("=== MAGIC CARD TEST ===")
	var card rfid2.Card
	for {
		c, err := d.Select()
		if err == nil {
			card = c
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	hexLine("uid", card.UIDBytes())
	print("sak=")
	hex2(card.SAK)
	println()
	println("type:", card.Type().String())

	key := [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	var uid4 [4]byte
	copy(uid4[:], card.UIDBytes())

	if err := d.Authenticate(0, rfid2.KeyA, key, uid4); err != nil {
		println("auth0=fail", err.Error())
		return
	}
	println("auth0=ok")

	original, err := d.ReadBlock(0)
	if err != nil {
		println("read0=fail", err.Error())
		return
	}
	hexLine("block0", original[:])

	// Write the same bytes back.
	err = rawWrite(d, 0, original)
	print("write_block0=")
	if err == nil {
		println("accepted")
	} else {
		println("refused:", err.Error())
	}

	after, err := d.ReadBlock(0)
	if err != nil {
		println("read0_after=fail", err.Error())
		println("VERDICT: card may be in a bad state, re-place it on the coil")
		return
	}
	hexLine("block0_after", after[:])

	same := true
	for i := 0; i < 16; i++ {
		if after[i] != original[i] {
			same = false
		}
	}
	if !same {
		println("VERDICT: block 0 CHANGED - unexpected")
	} else if err == nil {
		println("VERDICT: MAGIC CARD (Gen2 - block 0 writable after auth)")
	} else {
		println("VERDICT: normal card (block 0 protected) or Gen1a magic (needs the 0x40 backdoor)")
	}
	_ = d.StopCrypto1()
	_ = d.Halt()
	println("=== END ===")
}
