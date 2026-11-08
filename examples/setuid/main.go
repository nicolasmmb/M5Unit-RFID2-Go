//go:build esp32

// Writes a NEW UID on a magic card (block 0) and proves it took effect.
//
// Layout of block 0 (MIFARE Classic 1K):
//
//	[0..3]  UID
//	[4]     BCC = UID0 ^ UID1 ^ UID2 ^ UID3
//	[5]     SAK   (0x08 for 1K)
//	[6..7]  ATQA  (0x04 0x00)
//	[8..15] manufacturer data (kept from the original block)
//
// DESTRUCTIVE on a normal card: block 0 is protected there, so the card refuses
// it. On a magic card the UID really changes.
package main

import (
	"time"

	"machine"

	rfid2 "github.com/nicolasmmb/M5Unit-RFID2-Go"
)

const hexDigit = "0123456789abcdef"

// newUID is what the card will present after the write.
var newUID = [4]byte{0xCA, 0xFE, 0xBA, 0xBE}

func hex2(b byte) { print(string(hexDigit[b>>4]), string(hexDigit[b&0x0f])) }

func hexLine(key string, b []byte) {
	print(key, "=")
	for _, v := range b {
		hex2(v)
	}
	println()
}

func selectCard(d *rfid2.Device) (rfid2.Card, bool) {
	for i := 0; i < 20; i++ {
		if c, err := d.Select(); err == nil {
			return c, true
		}
		time.Sleep(150 * time.Millisecond)
	}
	return rfid2.Card{}, false
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

	println("=== SET UID (magic card) ===")
	card, ok := selectCard(d)
	if !ok {
		println("no card")
		return
	}
	hexLine("uid_before", card.UIDBytes())

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
	hexLine("block0_before", original[:])

	// Build the new block 0: new UID, recomputed BCC, keep SAK/ATQA/manufacturer.
	blk := original
	copy(blk[0:4], newUID[:])
	blk[4] = newUID[0] ^ newUID[1] ^ newUID[2] ^ newUID[3]
	hexLine("block0_new", blk[:])

	// The library does the whole block 0: BCC recomputed, SAK/ATQA and the
	// manufacturer bytes kept, two-phase write, read-back verification.
	if err := d.SetUID(card, newUID); err != nil {
		println("setuid=fail", err.Error())
		println("VERDICT: not a magic card, or it needs the 0x40 backdoor")
		return
	}
	println("setuid=ok")

	if back, err := d.ReadBlock(0); err == nil {
		hexLine("block0_after", back[:])
	} else {
		println("read0_after=fail", err.Error())
	}

	_ = d.StopCrypto1()
	_ = d.Halt()
	time.Sleep(300 * time.Millisecond)

	// The card must now present the new UID.
	if c, ok := selectCard(d); ok {
		hexLine("uid_after_select", c.UIDBytes())
		same := len(c.UIDBytes()) == 4 && c.UID[0] == newUID[0] && c.UID[1] == newUID[1] &&
			c.UID[2] == newUID[2] && c.UID[3] == newUID[3]
		if same {
			println("VERDICT: PASS - the card presents the new UID")
		} else {
			println("NOTE: still the old UID in this session; lift and re-place the card, then check again")
		}
	} else {
		println("re-select failed; lift and re-place the card")
	}
	println("=== END ===")
}
