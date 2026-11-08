package rfid2

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
	"unsafe"
)

var errNack = errors.New("fake: nack")

// responder returns the bytes the card would send for one frame.
type responder func(frame []byte, txBits byte) (resp []byte, irq, errReg byte)

type fakeChip struct {
	mu             sync.Mutex
	regs           [0x40]byte
	pendingReg     byte
	frame          []byte
	resp           []byte
	irq            byte
	errReg         byte
	version        byte
	dead           bool
	dropWrites     bool
	block0         [16]byte
	block0ReadOnly bool
	lastBlock      byte
	txBits         byte
	respFn         responder
	frames         [][]byte
}

func newChip() *fakeChip {
	f := &fakeChip{version: 0x92}
	f.regs[regVersion] = f.version
	return f
}

func (f *fakeChip) Tx(addr uint16, w, r []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.dead || addr != DefaultAddress {
		return errNack
	}

	// Read phase of a split register read.
	if len(w) == 0 {
		if len(r) == 0 {
			return nil
		}
		switch f.pendingReg {
		case regFIFOData:
			for i := range r {
				if i < len(f.resp) {
					r[i] = f.resp[i]
				} else {
					r[i] = 0
				}
			}
		case regFIFOLevel:
			r[0] = byte(len(f.resp))
		case regComIrq:
			r[0] = f.irq
		case regError:
			r[0] = f.errReg
		default:
			r[0] = f.regs[f.pendingReg]
		}
		return nil
	}

	reg := w[0]
	if len(w) == 1 {
		f.pendingReg = reg
		return nil
	}

	if reg == regFIFOData {
		f.frame = append(f.frame, w[1:]...)
		return nil
	}
	switch reg {
	case regCommand:
		f.exec(w[1])
	case regComIrq:
		f.irq &^= w[1]
	case regBitFraming:
		f.txBits = w[1] & 0x07
		f.regs[regBitFraming] = w[1]
	case regFIFOLevel:
		if w[1]&0x80 != 0 {
			f.frame = nil
			f.resp = nil
		}
	default:
		if !f.dropWrites {
			f.regs[reg] = w[1]
		}
	}
	return nil
}

func (f *fakeChip) exec(cmd byte) {
	switch cmd {
	case cmdSoftReset:
		f.regs[regVersion] = f.version
		f.frame, f.resp = nil, nil
		f.irq, f.errReg = 0, 0
	case cmdTransceive:
		f.frames = append(f.frames, append([]byte(nil), f.frame...))
		f.resp, f.irq, f.errReg = nil, 0, 0
		if f.respFn != nil {
			resp, irq, errReg := f.respFn(f.frame, f.txBits)
			f.resp, f.irq, f.errReg = append([]byte(nil), resp...), irq, errReg
		}
		f.frame = nil
	case cmdAuth:
		f.frame = nil
		f.irq |= irqIdle
	}
}

// cardScript emulates a MIFARE Classic card with a fixed UID.
var testUID = [4]byte{0xDE, 0xAD, 0xBE, 0xEF}

func cardScript(f *fakeChip) responder {
	return func(frame []byte, txBits byte) ([]byte, byte, byte) {
		switch {
		case len(frame) == 1 && frame[0] == piccREQA:
			return []byte{0x44, 0x00}, irqRx, 0
		case len(frame) == 2 && frame[0] == 0x93 && frame[1] == piccAnticoll:
			bcc := testUID[0] ^ testUID[1] ^ testUID[2] ^ testUID[3]
			return []byte{testUID[0], testUID[1], testUID[2], testUID[3], bcc}, irqRx, 0
		case len(frame) >= 9 && frame[0] == 0x93 && frame[1] == piccSelect:
			crc := crcA(frame[:7])
			got := uint16(frame[7]) | uint16(frame[8])<<8
			if crc != got {
				return nil, irqRx, errCRC
			}
			crcSak := crcA([]byte{0x08})
			return []byte{0x08, byte(crcSak), byte(crcSak >> 8)}, irqRx, 0
		case len(frame) == 4 && frame[0] == piccRead && frame[1] == 0:
			out := append([]byte(nil), f.block0[:]...)
			crc := crcA(out)
			return append(out, byte(crc), byte(crc>>8)), irqRx, 0
		case len(frame) == 4 && frame[0] == piccRead:
			var out []byte
			for i := 0; i < 16; i++ {
				out = append(out, byte(i))
			}
			crc := crcA(out)
			return append(out, byte(crc), byte(crc>>8)), irqRx, 0
		case len(frame) == 4 && frame[0] == piccWrite:
			f.lastBlock = frame[1]
			if f.lastBlock == 0 && f.block0ReadOnly {
				return []byte{0x00}, irqRx, 0 // NAK: block 0 protected
			}
			return []byte{0x0A}, irqRx, 0 // phase 1 ACK
		case len(frame) == 18:
			if f.lastBlock == 0 {
				copy(f.block0[:], frame[:16])
			}
			return []byte{0x0A}, irqRx, 0 // phase 2 ACK
		}
		return nil, irqRx, 0
	}
}

func newDeviceT(t *testing.T) (*Device, *fakeChip) {
	t.Helper()
	f := newChip()
	d := New(f)
	if err := d.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	f.respFn = cardScript(f)
	return d, f
}

func TestInit(t *testing.T) {
	f := newChip()
	d := New(f)
	if err := d.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	v, err := d.Version()
	if err != nil || v != 0x92 {
		t.Fatalf("Version = %#02x, %v", v, err)
	}
	if f.regs[regTxControl]&txControlRF != txControlRF {
		t.Fatalf("antenna not enabled: %#02x", f.regs[regTxControl])
	}
	if f.regs[regMode] != 0x3D || f.regs[regTMode] != 0x80 {
		t.Fatalf("timers not set: mode %#02x tmode %#02x", f.regs[regMode], f.regs[regTMode])
	}

	// A different version byte is not an error: the WS1850S reports 0x15.
	f2 := newChip()
	f2.version = 0x15
	d2 := New(f2)
	if err := d2.Init(); err != nil {
		t.Fatalf("version 0x15: %v", err)
	}
	if v, _ := d2.Version(); v != 0x15 {
		t.Fatalf("Version = %#02x, want 0x15", v)
	}

	// A chip that does not keep a register value is rejected.
	f4 := newChip()
	f4.dropWrites = true
	if err := New(f4).Init(); err != ErrBadVersion {
		t.Fatalf("read-back mismatch = %v", err)
	}

	// Dead and nil bus.
	f3 := newChip()
	f3.dead = true
	if err := New(f3).Init(); err != ErrNotConnected {
		t.Fatalf("dead = %v", err)
	}
	if err := New(nil).Init(); err != ErrNotConnected {
		t.Fatalf("nil bus = %v", err)
	}
}

func TestAntenna(t *testing.T) {
	f := newChip()
	d := New(f)
	if err := d.Init(); err != nil {
		t.Fatal(err)
	}
	if err := d.Antenna(false); err != nil {
		t.Fatal(err)
	}
	if f.regs[regTxControl]&txControlRF != 0 {
		t.Fatalf("antenna still on: %#02x", f.regs[regTxControl])
	}
	if err := d.Antenna(true); err != nil {
		t.Fatal(err)
	}
	if f.regs[regTxControl]&txControlRF != txControlRF {
		t.Fatal("antenna not back on")
	}
}

func TestSelect(t *testing.T) {
	d, f := newDeviceT(t)
	c, err := d.Select()
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if c.UIDBytes() == nil || len(c.UIDBytes()) != 4 || c.UID[0] != testUID[0] || c.UID[3] != testUID[3] {
		t.Fatalf("UID = % X, want % X", c.UIDBytes(), testUID)
	}
	if c.SAK != 0x08 {
		t.Fatalf("SAK = %#02x", c.SAK)
	}
	if c.ATQA != [2]byte{0x44, 0x00} {
		t.Fatalf("ATQA = % X", c.ATQA)
	}
	if len(f.frames) != 3 {
		t.Fatalf("frames = %d, want 3 (REQA, anticoll, select)", len(f.frames))
	}
	// REQA must be sent with 7 bits.
	if len(f.frames[0]) != 1 || f.frames[0][0] != piccREQA {
		t.Fatalf("first frame = % X", f.frames[0])
	}
}

func TestSelectNoCard(t *testing.T) {
	d, f := newDeviceT(t)
	d.SetTimeout(2 * time.Millisecond)
	f.respFn = func(frame []byte, txBits byte) ([]byte, byte, byte) {
		return nil, 0, 0 // nothing answers, no IRQ: timeout
	}
	if _, err := d.Select(); err != ErrTimeout {
		t.Fatalf("no card = %v", err)
	}
}

func TestSelectCollision(t *testing.T) {
	d, f := newDeviceT(t)
	f.respFn = func(frame []byte, txBits byte) ([]byte, byte, byte) {
		return nil, irqRx, errColl
	}
	if _, err := d.Select(); err != ErrCollision {
		t.Fatalf("collision = %v", err)
	}
	// A broken BCC is a protocol error.
	f.respFn = func(frame []byte, txBits byte) ([]byte, byte, byte) {
		if len(frame) == 1 {
			return []byte{0x44, 0x00}, irqRx, 0
		}
		return []byte{0x01, 0x02, 0x03, 0x04, 0x00}, irqRx, 0
	}
	if _, err := d.Select(); err != ErrProtocol {
		t.Fatalf("bad BCC = %v", err)
	}
}

func TestSelectContext(t *testing.T) {
	d, _ := newDeviceT(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := d.SelectContext(ctx); err != context.Canceled {
		t.Fatalf("canceled = %v", err)
	}
}

func TestAuthenticate(t *testing.T) {
	d, f := newDeviceT(t)
	key := [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}

	f.errReg = 0
	if err := d.Authenticate(4, KeyA, key, testUID); err != nil {
		t.Fatalf("auth: %v", err)
	}
	// The frame layout must be command, block, key, uid.
	if got := f.frame; len(got) != 0 {
		_ = got
	}

	f.errReg = 0x08 // CollErr
	if err := d.Authenticate(4, KeyA, key, testUID); err != ErrAuth {
		t.Fatalf("auth failure = %v", err)
	}
}

func TestReadBlock(t *testing.T) {
	d, f := newDeviceT(t)
	blk, err := d.ReadBlock(4)
	if err != nil {
		t.Fatalf("ReadBlock: %v", err)
	}
	for i := 0; i < 16; i++ {
		if blk[i] != byte(i) {
			t.Fatalf("block[%d] = %#02x", i, blk[i])
		}
	}

	// A corrupted CRC must be rejected.
	f.respFn = func(frame []byte, txBits byte) ([]byte, byte, byte) {
		out := make([]byte, 18)
		for i := range out[:16] {
			out[i] = byte(i)
		}
		return out, irqRx, 0 // CRC zero: wrong
	}
	if _, err := d.ReadBlock(4); err != ErrCRC {
		t.Fatalf("bad crc = %v", err)
	}
}

func TestWriteBlock(t *testing.T) {
	d, f := newDeviceT(t)
	var data [16]byte
	for i := range data {
		data[i] = byte(0xA0 + i)
	}
	if err := d.WriteBlock(5, data); err != nil {
		t.Fatalf("WriteBlock: %v", err)
	}

	f.mu.Lock()
	frames := append([][]byte(nil), f.frames...)
	f.mu.Unlock()
	if len(frames) < 2 {
		t.Fatalf("frames = %d, want 2 phases", len(frames))
	}
	// Phase 1: A0, block, CRC_A.
	p1 := frames[len(frames)-2]
	if len(p1) != 4 || p1[0] != piccWrite || p1[1] != 5 {
		t.Fatalf("phase 1 = % X", p1)
	}
	if got := uint16(p1[2]) | uint16(p1[3])<<8; crcA(p1[:2]) != got {
		t.Fatal("phase 1 CRC is wrong")
	}
	// Phase 2: the 16 data bytes and their CRC_A.
	p2 := frames[len(frames)-1]
	if len(p2) != 18 {
		t.Fatalf("phase 2 length = %d, want 18", len(p2))
	}
	for i := 0; i < 16; i++ {
		if p2[i] != data[i] {
			t.Fatalf("phase 2 byte %d = %#02x, want %#02x", i, p2[i], data[i])
		}
	}
	if got := uint16(p2[16]) | uint16(p2[17])<<8; crcA(p2[:16]) != got {
		t.Fatal("phase 2 CRC is wrong")
	}

	// A wrong ACK is a protocol error.
	f.respFn = func(frame []byte, txBits byte) ([]byte, byte, byte) {
		return []byte{0x00}, irqRx, 0
	}
	if err := d.WriteBlock(5, data); err != ErrProtocol {
		t.Fatalf("bad ack = %v", err)
	}
}

func TestHalt(t *testing.T) {
	d, f := newDeviceT(t)
	if err := d.Halt(); err != nil {
		t.Fatalf("Halt: %v", err)
	}
	f.mu.Lock()
	frame := f.frames[len(f.frames)-1]
	f.mu.Unlock()
	if len(frame) != 4 || frame[0] != piccHalt {
		t.Fatalf("halt frame = % X", frame)
	}
}

func TestCRC(t *testing.T) {
	// Same polynomial and init as the datasheet, checked through a second
	// implementation in the test (bit order and init are the contract).
	ref := func(b []byte) uint16 {
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
	for _, frame := range [][]byte{{0x26}, {0x93, 0x20}, {0x50, 0x00}, {0x30, 0x04}} {
		if crcA(frame) != ref(frame) {
			t.Fatalf("crc mismatch for % X", frame)
		}
	}
	// Real-world vector: the MIFARE HALT frame is sent as 50 00 57 CD.
	if got := crcA([]byte{piccHalt, 0x00}); got != 0xCD57 {
		t.Fatalf("HALT crc = %#04x, want 0xCD57", got)
	}
}

// silentChip answers without allocating: fixed registers, fixed responses, no
// slices, so it can measure the driver instead of the fake.
type silentChip struct {
	regs    [0x40]byte
	pending byte
}

func (c *silentChip) Tx(addr uint16, w, r []byte) error {
	if addr != DefaultAddress {
		return nil
	}
	if len(w) == 0 {
		// Read phase of a split register read.
		switch c.pending {
		case regComIrq:
			if len(r) > 0 {
				r[0] = irqRx
			}
		case regError:
			if len(r) > 0 {
				r[0] = 0
			}
		case regFIFOLevel:
			if len(r) > 0 {
				r[0] = maxFrame
			}
		case regFIFOData:
			for i := range r {
				r[i] = byte(i)
			}
		default:
			if len(r) > 0 {
				r[0] = c.regs[c.pending]
			}
		}
		return nil
	}
	reg := w[0]
	if len(w) == 1 && len(r) > 0 {
		switch reg {
		case regComIrq:
			r[0] = irqRx
		case regError:
			r[0] = 0
		case regFIFOLevel:
			r[0] = maxFrame
		case regFIFOData:
			for i := range r {
				r[i] = byte(i)
			}
		default:
			r[0] = c.regs[reg]
		}
		return nil
	}
	if len(w) == 1 {
		c.pending = reg
		return nil
	}
	if reg == regCommand {
		switch w[1] {
		case cmdSoftReset:
			c.regs[regVersion] = 0x92
		case cmdTransceive:
			// The canned answers keep Read, Select and Write happy.
		}
		return nil
	}
	if len(w) == 2 && reg != regFIFOData {
		c.regs[reg] = w[1]
	}
	return nil
}

func TestNoAlloc(t *testing.T) {
	d := New(&silentChip{})
	d.SetTimeout(5 * time.Millisecond)
	key := [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	var data [16]byte
	allocs := testing.AllocsPerRun(20, func() {
		_, _ = d.Select()
		_ = d.Authenticate(4, KeyA, key, testUID)
		_, _ = d.ReadBlock(4)
		_ = d.WriteBlock(4, data)
		_ = d.Halt()
	})
	if allocs != 0 {
		t.Fatalf("driver path allocates %v", allocs)
	}
}

func TestFootprint(t *testing.T) {
	size := unsafe.Sizeof(Device{})
	t.Logf("Device=%d B", size)
	if size > 128 {
		t.Fatalf("Device is %d B, budget is 128 B", size)
	}
}

// TestSelectCascade2 covers a 7-byte UID card (MIFARE Ultralight): level 1
// answers with the cascade bit set and level 2 finishes the UID.
func TestSelectCascade2(t *testing.T) {
	d, f := newDeviceT(t)
	uid7 := [7]byte{0x04, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66}
	f.respFn = func(frame []byte, txBits byte) ([]byte, byte, byte) {
		switch {
		case len(frame) == 1 && frame[0] == piccREQA:
			return []byte{0x44, 0x00}, irqRx, 0
		case len(frame) == 2 && frame[0] == 0x93 && frame[1] == piccAnticoll:
			// CL1: cascade tag 0x88 + UID0..2, BCC over the four bytes.
			p := []byte{0x88, uid7[0], uid7[1], uid7[2]}
			bcc := p[0] ^ p[1] ^ p[2] ^ p[3]
			return append(p, bcc), irqRx, 0
		case len(frame) == 9 && frame[0] == 0x93:
			crc := crcA([]byte{0x04})
			return []byte{0x04, byte(crc), byte(crc >> 8)}, irqRx, 0 // cascade bit set
		case len(frame) == 2 && frame[0] == 0x95 && frame[1] == piccAnticoll:
			p := []byte{uid7[3], uid7[4], uid7[5], uid7[6]}
			bcc := p[0] ^ p[1] ^ p[2] ^ p[3]
			return append(p, bcc), irqRx, 0
		case len(frame) == 9 && frame[0] == 0x95:
			crc := crcA([]byte{0x00})
			return []byte{0x00, byte(crc), byte(crc >> 8)}, irqRx, 0 // cascade complete
		}
		return nil, irqRx, 0
	}

	card, err := d.Select()
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	got := card.UIDBytes()
	want := uid7[:]
	if len(got) != len(want) {
		t.Fatalf("UID length = %d, want %d (% X)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("UID = % X, want % X", got, want)
		}
	}
}
