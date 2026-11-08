package rfid2

import (
	"testing"
	"time"
)

const timeMillisecond = time.Millisecond

func TestCardType(t *testing.T) {
	cases := []struct {
		sak  byte
		want Type
	}{
		{0x09, TypeMifareMini},
		{0x08, TypeMifare1K},
		{0x18, TypeMifare4K},
		{0x00, TypeMifareUL},
		{0x10, TypeMifarePlus},
		{0x11, TypeMifarePlus},
		{0x01, TypeTNP3XXX},
		{0x20, TypeISO14443_4},
		{0x40, TypeISO18092},
		{0x24, TypeNotComplete}, // cascade bit set wins
		{0x44, TypeNotComplete},
		{0x60, TypeUnknown},
	}
	for _, c := range cases {
		got := Card{SAK: c.sak}.Type()
		if got != c.want {
			t.Fatalf("SAK %#02x: type = %v, want %v", c.sak, got, c.want)
		}
	}

	// The names must be stable: they are printed in logs.
	if TypeMifare1K.String() != "MIFARE Classic 1K" || TypeUnknown.String() != "unknown" {
		t.Fatal("Type.String changed")
	}
}

// TestRealCardFromHardware locks the mapping with the card measured on the
// unit: MIFARE Classic 1K, SAK 0x08, ATQA 0x04 0x00.
func TestRealCardFromHardware(t *testing.T) {
	c := Card{SAK: 0x08, ATQA: [2]byte{0x04, 0x00}, Length: 4}
	if got := c.Type(); got != TypeMifare1K {
		t.Fatalf("type = %v, want MIFARE Classic 1K", got)
	}
}

// TestWriteBlockRejectsUnsafeBlocks protects the card: block 0 and the sector
// trailers must never be written by this driver.
func TestWriteBlockRejectsUnsafeBlocks(t *testing.T) {
	d, _ := newDeviceT(t)
	var data [16]byte
	for _, blk := range []byte{0, 3, 7, 11, 15} {
		if err := d.WriteBlock(blk, data); err != ErrBadBlock {
			t.Fatalf("block %d = %v, want ErrBadBlock", blk, err)
		}
	}
	// A data block is accepted.
	if err := d.WriteBlock(4, data); err != nil {
		t.Fatalf("block 4 = %v", err)
	}
}

// TestAuthFailureDuringRead covers a card that leaves the field mid-operation.
func TestAuthFailureDuringRead(t *testing.T) {
	d, f := newDeviceT(t)
	key := [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	f.errReg = 0x02 // ParityErr, one of the bits the official treats as failure
	if err := d.Authenticate(4, KeyA, key, testUID); err != ErrAuth {
		t.Fatalf("auth with ParityErr = %v, want ErrAuth", err)
	}
	// Timeout when the card does not answer at all.
	d.SetTimeout(2 * timeMillisecond)
	f.respFn = func(frame []byte, txBits byte) ([]byte, byte, byte) { return nil, 0, 0 }
	if _, err := d.Select(); err != ErrTimeout {
		t.Fatalf("silent card = %v, want ErrTimeout", err)
	}
}

// TestFatalErrorReg covers the mask the official driver rejects (0x13).
func TestFatalErrorReg(t *testing.T) {
	d, f := newDeviceT(t)
	for _, bit := range []byte{0x01, 0x02, 0x10} {
		bit := bit
		f.respFn = func(frame []byte, txBits byte) ([]byte, byte, byte) {
			return nil, irqRx, bit
		}
		if _, err := d.Select(); err != ErrProtocol {
			t.Fatalf("ErrorReg %#02x = %v, want ErrProtocol", bit, err)
		}
	}
	f.respFn = func(frame []byte, txBits byte) ([]byte, byte, byte) {
		return nil, irqRx, 0x08 // CollErr
	}
	if _, err := d.Select(); err != ErrCollision {
		t.Fatalf("CollErr = %v, want ErrCollision", err)
	}
}

// TestSetUID covers the magic-card writer: BCC recomputation, SAK/ATQA and
// manufacturer bytes preserved, and ErrNotMagic when the card refuses.
func TestSetUID(t *testing.T) {
	d, f := newDeviceT(t)
	card := Card{SAK: 0x08, Length: 4, UID: [10]byte{0xE0, 0xDE, 0x9A, 0x7F}}

	// Block 0 with the original content.
	var blk [16]byte
	copy(blk[:], []byte{0xE0, 0xDE, 0x9A, 0x7F, 0xDB, 0x08, 0x04, 0x00, 0x47, 0x59, 0x55, 0xD1, 0x41, 0x10, 0x36, 0x07})
	f.block0 = blk

	newUID := [4]byte{0xDE, 0xAD, 0xBE, 0xEF}
	if err := d.SetUID(card, newUID); err != nil {
		t.Fatalf("SetUID: %v", err)
	}
	want := blk
	copy(want[0:4], newUID[:])
	want[4] = 0xDE ^ 0xAD ^ 0xBE ^ 0xEF // 0x22
	if f.block0 != want {
		t.Fatalf("block 0 = % X, want % X", f.block0, want)
	}
	if want[4] != 0x22 || want[5] != 0x08 || want[6] != 0x04 || want[7] != 0x00 {
		t.Fatalf("layout wrong: % X", want)
	}

	// All zero / all FF UIDs are refused.
	if err := d.SetUID(card, [4]byte{}); err != ErrBadUID {
		t.Fatalf("zero uid = %v", err)
	}
	if err := d.SetUID(card, [4]byte{0xFF, 0xFF, 0xFF, 0xFF}); err != ErrBadUID {
		t.Fatalf("ff uid = %v", err)
	}

	// A card that refuses block 0 is not magic.
	f.block0ReadOnly = true
	if err := d.SetUID(card, newUID); err != ErrNotMagic {
		t.Fatalf("read-only block 0 = %v", err)
	}

	// A 7-byte UID card is not supported here.
	card.Length = 7
	if err := d.SetUID(card, newUID); err != ErrBadUID {
		t.Fatalf("7-byte uid card = %v", err)
	}
}
