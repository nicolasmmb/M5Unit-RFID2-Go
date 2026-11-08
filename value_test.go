package rfid2

import "testing"

func TestFormatValue(t *testing.T) {
	for _, tc := range []struct {
		value int32
		block byte
	}{
		{100, 5},
		{0, 4},
		{-1, 8},
		{2147483647, 12},
		{-2147483648, 16},
	} {
		b := FormatValue(tc.value, tc.block)
		// value, complement, value, address x4
		got, err := ValueFromBlock(b, tc.block)
		if err != nil {
			t.Fatalf("value %d: %v", tc.value, err)
		}
		if got != tc.value {
			t.Fatalf("value = %d, want %d", got, tc.value)
		}
		v := uint32(tc.value)
		comp := ^v
		if b[4] != byte(comp) || b[5] != byte(comp>>8) {
			t.Fatalf("complement wrong for %d: % X", tc.value, b[4:8])
		}
		for i := 12; i < 16; i++ {
			if b[i] != tc.block {
				t.Fatalf("address byte %d = %d, want %d", i, b[i], tc.block)
			}
		}
	}
}

func TestValueFromBlockRejectsJunk(t *testing.T) {
	good := FormatValue(42, 5)
	if _, err := ValueFromBlock(good, 5); err != nil {
		t.Fatalf("good block: %v", err)
	}
	// Wrong block number in the address bytes.
	if _, err := ValueFromBlock(good, 6); err != ErrNotValueBlock {
		t.Fatalf("wrong address = %v", err)
	}
	// Broken complement.
	bad := good
	bad[4] ^= 0xFF
	if _, err := ValueFromBlock(bad, 5); err != ErrNotValueBlock {
		t.Fatalf("broken complement = %v", err)
	}
	// Broken repeat.
	bad = good
	bad[8] ^= 0x01
	if _, err := ValueFromBlock(bad, 5); err != ErrNotValueBlock {
		t.Fatalf("broken repeat = %v", err)
	}
	// A plain data block is not a value block.
	if _, err := ValueFromBlock([16]byte{1, 2, 3}, 5); err != ErrNotValueBlock {
		t.Fatalf("data block = %v", err)
	}
}

func TestValueCommandsFrames(t *testing.T) {
	d, f := newDeviceT(t)
	// The fake answer for value commands: ACK.
	// Phase 1 answers ACK; phase 2 (the 6-byte operand frame) never answers,
	// which the driver must accept as success.
	f.respFn = func(frame []byte, txBits byte) ([]byte, byte, byte) {
		if len(frame) == 4 {
			return []byte{0x0A}, irqRx, 0
		}
		return nil, 0, 0 // timeout
	}

	if err := d.ValueIncrement(5, 10); err != nil {
		t.Fatalf("increment: %v", err)
	}
	f.mu.Lock()
	inc := append([]byte(nil), f.frames[len(f.frames)-1]...)
	f.mu.Unlock()
	// Two phases: command frame then operand frame.
	f.mu.Lock()
	n := len(f.frames)
	op := append([]byte(nil), f.frames[n-1]...)
	cmdFrame := append([]byte(nil), f.frames[n-2]...)
	f.mu.Unlock()
	if len(cmdFrame) != 4 || cmdFrame[0] != piccValueIncrement || cmdFrame[1] != 5 {
		t.Fatalf("increment command frame = % X", cmdFrame)
	}
	if got := uint16(cmdFrame[2]) | uint16(cmdFrame[3])<<8; crcA(cmdFrame[:2]) != got {
		t.Fatal("command CRC wrong")
	}
	if len(op) != 6 || op[0] != 10 || op[1] != 0 || op[2] != 0 || op[3] != 0 {
		t.Fatalf("operand not little endian: % X", op)
	}
	if got := uint16(op[4]) | uint16(op[5])<<8; crcA(op[:4]) != got {
		t.Fatal("operand CRC wrong")
	}
	_ = inc

	if err := d.ValueDecrement(5, 30); err != nil {
		t.Fatalf("decrement: %v", err)
	}
	if err := d.ValueRestore(5); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if err := d.ValueTransfer(5); err != nil {
		t.Fatalf("transfer: %v", err)
	}
	f.mu.Lock()
	tr := append([]byte(nil), f.frames[len(f.frames)-1]...)
	f.mu.Unlock()
	if len(tr) != 4 || tr[0] != piccValueTransfer || tr[1] != 5 {
		t.Fatalf("transfer frame = % X", tr)
	}

	// Unsafe blocks are refused without touching the bus.
	for _, blk := range []byte{0, 3, 7} {
		if err := d.ValueTransfer(blk); err != ErrBadBlock {
			t.Fatalf("transfer block %d = %v", blk, err)
		}
		if err := d.ValueIncrement(blk, 1); err != ErrBadBlock {
			t.Fatalf("increment block %d = %v", blk, err)
		}
	}
}

func TestValueNoAlloc(t *testing.T) {
	// The recording fake allocates by itself; measure with the quiet one.
	c := &quietChip{}
	d := New(c)
	if err := d.Init(); err != nil {
		t.Fatal(err)
	}
	allocs := testing.AllocsPerRun(50, func() {
		_ = d.ValueIncrement(5, 1)
		_ = d.ValueTransfer(5)
		b := FormatValue(7, 5)
		_, _ = ValueFromBlock(b, 5)
	})
	if allocs != 0 {
		t.Fatalf("value path allocates %v", allocs)
	}
}
