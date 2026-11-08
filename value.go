package rfid2

// MIFARE Classic value blocks and the four value commands.
//
// A value block holds a signed 32-bit value three times: the value, its bitwise
// complement and the value again, plus the block address repeated four times.
// The card itself validates that layout, which is why the driver always builds
// it with FormatValue instead of letting the caller assemble the bytes.
const (
	piccValueIncrement = 0xC1
	piccValueDecrement = 0xC0
	piccValueRestore   = 0xC2
	piccValueTransfer  = 0xB0
)

// FormatValue builds the 16-byte value block for value at block.
func FormatValue(value int32, block byte) [16]byte {
	var b [16]byte
	v := uint32(value)
	putLE := func(off int, x uint32) {
		b[off] = byte(x)
		b[off+1] = byte(x >> 8)
		b[off+2] = byte(x >> 16)
		b[off+3] = byte(x >> 24)
	}
	putLE(0, v)
	putLE(4, ^v)
	putLE(8, v)
	b[12], b[13], b[14], b[15] = block, block, block, block
	return b
}

// ValueFromBlock validates a value block and returns its value.
//
// It checks the complement and the address bytes, so a block that is not a
// value block (or is corrupted) returns ErrNotValueBlock instead of a number.
func ValueFromBlock(b [16]byte, block byte) (int32, error) {
	getLE := func(off int) uint32 {
		return uint32(b[off]) | uint32(b[off+1])<<8 | uint32(b[off+2])<<16 | uint32(b[off+3])<<24
	}
	v := getLE(0)
	if getLE(4) != ^v || getLE(8) != v {
		return 0, ErrNotValueBlock
	}
	if b[12] != block || b[13] != block || b[14] != block || b[15] != block {
		return 0, ErrNotValueBlock
	}
	return int32(v), nil
}

// ValueIncrement stages block += value. Call ValueTransfer to persist it.
func (d *Device) ValueIncrement(block byte, value int32) error {
	return d.valueOp(piccValueIncrement, block, value)
}

// ValueDecrement stages block -= value. Call ValueTransfer to persist it.
func (d *Device) ValueDecrement(block byte, value int32) error {
	return d.valueOp(piccValueDecrement, block, value)
}

// ValueRestore stages block = its current value, discarding a pending
// increment or decrement. Call ValueTransfer to persist it.
func (d *Device) ValueRestore(block byte) error {
	return d.valueOp(piccValueRestore, block, 0)
}

// ValueTransfer writes the staged value into the block.
func (d *Device) ValueTransfer(block byte) error {
	if err := checkDataBlock(block); err != nil {
		return err
	}
	d.tx[0] = piccValueTransfer
	d.tx[1] = block
	crc := crcA(d.tx[:2])
	d.tx[2], d.tx[3] = byte(crc), byte(crc>>8)
	return d.expectAck(d.tx[:4])
}

// valueOp sends a value command, following MIFARE_TwoStepHelper from the
// official driver:
//
//	phase 1: command + block + CRC_A   -> the card answers ACK (0x0A, 4 bits)
//	phase 2: 4-byte operand + CRC_A    -> the card does NOT answer
//
// A timeout in phase 2 is success: the card accepted the operand silently.
func (d *Device) valueOp(cmd, block byte, value int32) error {
	if err := checkDataBlock(block); err != nil {
		return err
	}
	d.tx[0] = cmd
	d.tx[1] = block
	crc := crcA(d.tx[:2])
	d.tx[2], d.tx[3] = byte(crc), byte(crc>>8)
	if err := d.expectAck(d.tx[:4]); err != nil {
		return err
	}

	v := uint32(value)
	d.tx[0], d.tx[1], d.tx[2], d.tx[3] = byte(v), byte(v>>8), byte(v>>16), byte(v>>24)
	crc = crcA(d.tx[:4])
	d.tx[4], d.tx[5] = byte(crc), byte(crc>>8)
	if err := d.expectAck(d.tx[:6]); err != nil {
		if err == ErrTimeout {
			return nil // accepted silently, like acceptTimeout in the official code
		}
		return err
	}
	return nil
}

// checkDataBlock refuses block 0 (manufacturer) and the sector trailers.
func checkDataBlock(block byte) error {
	if block == 0 || block%4 == 3 {
		return ErrBadBlock
	}
	return nil
}
