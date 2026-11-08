package rfid2

import "testing"

// quietChip answers like a live chip without allocating: fixed arrays, no
// slices, no append. It supports REQA, anticollision, SELECT, auth, block 0
// read/write and block reads, so the driver's hot paths can be measured.
type quietChip struct {
	regs     [0x40]byte
	pending  byte
	frame    [24]byte
	frameLen int
	resp     [24]byte
	respLen  int
	irq      byte
	errReg   byte
	block0   [16]byte
}

func (c *quietChip) Tx(addr uint16, w, r []byte) error {
	if addr != DefaultAddress {
		return errNack
	}
	if len(w) == 0 { // register read
		if len(r) == 0 {
			return nil
		}
		switch c.pending {
		case regComIrq:
			r[0] = c.irq
		case regError:
			r[0] = c.errReg
		case regFIFOLevel:
			r[0] = byte(c.respLen)
		case regFIFOData:
			for i := range r {
				if i < c.respLen {
					r[i] = c.resp[i]
				} else {
					r[i] = 0
				}
			}
		case regControl, regColl:
			r[0] = 0
		default:
			r[0] = c.regs[c.pending]
		}
		return nil
	}
	reg := w[0]
	if len(w) == 1 {
		c.pending = reg
		return nil
	}
	if reg == regFIFOData {
		for _, b := range w[1:] {
			if c.frameLen < len(c.frame) {
				c.frame[c.frameLen] = b
				c.frameLen++
			}
		}
		return nil
	}
	switch reg {
	case regCommand:
		c.exec(w[1])
	case regComIrq:
		c.irq &^= w[1]
	case regFIFOLevel:
		if w[1]&0x80 != 0 {
			c.frameLen, c.respLen = 0, 0
		}
	default:
		c.regs[reg] = w[1]
	}
	return nil
}

func (c *quietChip) exec(cmd byte) {
	switch cmd {
	case cmdSoftReset:
		c.regs[regVersion] = 0x92
		c.frameLen, c.respLen = 0, 0
	case cmdAuth:
		c.irq, c.errReg = irqIdle, 0
		c.frameLen = 0
	case cmdTransceive:
		c.irq, c.errReg, c.respLen = irqRx, 0, 0
		f := c.frame[:c.frameLen]
		switch {
		case f[0] == piccValueIncrement || f[0] == piccValueDecrement ||
			f[0] == piccValueRestore || f[0] == piccValueTransfer:
			c.resp[0], c.respLen = 0x0A, 1
		case len(f) == 1 && f[0] == piccREQA:
			c.resp[0], c.resp[1], c.respLen = 0x04, 0x00, 2
		case len(f) == 2 && f[0] == 0x93 && f[1] == piccAnticoll:
			uid := [4]byte{0xDE, 0xAD, 0xBE, 0xEF}
			bcc := uid[0] ^ uid[1] ^ uid[2] ^ uid[3]
			c.resp[0], c.resp[1], c.resp[2], c.resp[3], c.resp[4] = uid[0], uid[1], uid[2], uid[3], bcc
			c.respLen = 5
		case len(f) == 9 && f[1] == 0x70:
			crc := crcA([]byte{0x08})
			c.resp[0], c.resp[1], c.resp[2] = 0x08, byte(crc), byte(crc>>8)
			c.respLen = 3
		case len(f) == 4 && f[0] == piccRead && f[1] == 0:
			for i := 0; i < 16; i++ {
				c.resp[i] = c.block0[i]
			}
			crc := crcA(c.resp[:16])
			c.resp[16], c.resp[17], c.respLen = byte(crc), byte(crc>>8), 18
		case len(f) == 4 && f[0] == piccRead:
			for i := 0; i < 16; i++ {
				c.resp[i] = byte(i)
			}
			crc := crcA(c.resp[:16])
			c.resp[16], c.resp[17], c.respLen = byte(crc), byte(crc>>8), 18
		case len(f) == 4 && f[0] == piccWrite:
			c.resp[0], c.respLen = 0x0A, 1
		case len(f) == 18:
			for i := 0; i < 16; i++ {
				c.block0[i] = f[i]
			}
			c.resp[0], c.respLen = 0x0A, 1
		default:
			c.respLen = 0
		}
		c.frameLen = 0
	}
}

// TestNoAllocQuietChip measures the driver with a chip emulator that does not
// allocate: fixed arrays, no slices. The recording fake used by the other tests
// appends to slices, so measuring with it would measure the fake.
func TestNoAllocQuietChip(t *testing.T) {
	c := &quietChip{}
	d := New(c)
	if err := d.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	key := [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	var uid4 [4]byte
	var data [16]byte
	card := Card{SAK: 0x08, Length: 4, UID: [10]byte{0xDE, 0xAD, 0xBE, 0xEF}}
	copy(uid4[:], card.UID[:4])

	if n := testing.AllocsPerRun(10, func() { _, _ = d.Select() }); n != 0 {
		t.Errorf("Select: %v allocs", n)
	}
	if n := testing.AllocsPerRun(10, func() { _ = d.Authenticate(4, KeyA, key, uid4) }); n != 0 {
		t.Errorf("Authenticate: %v allocs", n)
	}
	if n := testing.AllocsPerRun(10, func() { _, _ = d.ReadBlock(4) }); n != 0 {
		t.Errorf("ReadBlock: %v allocs", n)
	}
	if n := testing.AllocsPerRun(10, func() { _ = d.WriteBlock(4, data) }); n != 0 {
		t.Errorf("WriteBlock: %v allocs", n)
	}
	if n := testing.AllocsPerRun(10, func() { _ = d.SetUID(card, [4]byte{0xCA, 0xFE, 0xBA, 0xBE}) }); n != 0 {
		t.Errorf("SetUID: %v allocs", n)
	}
	if n := testing.AllocsPerRun(10, func() { _ = d.SetAntennaGain(Gain38dB) }); n != 0 {
		t.Errorf("SetAntennaGain: %v allocs", n)
	}
	if n := testing.AllocsPerRun(10, func() { _ = d.Halt() }); n != 0 {
		t.Errorf("Halt: %v allocs", n)
	}
	t.Log("driver hot paths: 0 allocs")
}
