// Package rfid2 drives the M5Stack Unit RFID2 (SKU U031-B): a 13.56 MHz
// ISO/IEC 14443-A reader built on the WS1850S, which is register compatible
// with the MFRC522.
//
// The unit talks I2C at 0x28: one write of [register, value] or [register]
// followed by a read. This package is pure Go, uses fixed buffers and does not
// allocate per operation. It depends only on the two-argument I2C interface, so
// machine.I2C satisfies it on any chip.
package rfid2

import (
	"context"
	"errors"
	"time"
)

// I2C is the subset of machine.I2C used by the unit.
type I2C interface {
	Tx(addr uint16, w, r []byte) error
}

const (
	// DefaultAddress is the I2C address of the unit.
	DefaultAddress uint16 = 0x28

	// maxFrame is the largest frame this driver sends (a block write is
	// command + block + 16 data + 2 CRC = 20). The answer is at most 18.
	maxFrame = 20
)

// Errors returned by the driver. Sentinels: compare with errors.Is.
var (
	ErrNotConnected = errors.New("rfid2: unit not responding")
	ErrBadVersion   = errors.New("rfid2: unexpected chip version")
	ErrNoCard       = errors.New("rfid2: no card in the field")
	ErrCollision    = errors.New("rfid2: collision during anticollision")
	ErrAuth         = errors.New("rfid2: MIFARE authentication failed")
	ErrCRC          = errors.New("rfid2: CRC error")
	ErrProtocol     = errors.New("rfid2: protocol error")
	ErrTimeout      = errors.New("rfid2: timeout")
	ErrFrame        = errors.New("rfid2: frame too long")
	ErrBadBlock     = errors.New("rfid2: block out of range")
	ErrNotMagic     = errors.New("rfid2: block 0 refused, the card is not a magic card")
	ErrBadUID       = errors.New("rfid2: UID must be 4 bytes and not all 0x00 or all 0xFF")
)

// Register map of the MFRC522/WS1850S (datasheet chapter 9).
const (
	regCommand    = 0x01
	regComIEn     = 0x02
	regDivIEn     = 0x03
	regComIrq     = 0x04
	regDivIrq     = 0x05
	regError      = 0x06
	regStatus2    = 0x08
	regFIFOData   = 0x09
	regFIFOLevel  = 0x0A
	regControl    = 0x0C
	regBitFraming = 0x0D
	regColl       = 0x0E
	regMode       = 0x11
	regTxMode     = 0x12
	regRxMode     = 0x13
	regTxControl  = 0x14
	regTxASK      = 0x15
	regTMode      = 0x2A
	regTPrescaler = 0x2B
	regTReloadH   = 0x2C
	regTReloadL   = 0x2D
	regVersion    = 0x37
	regRFCfg      = 0x26 // receiver gain, bits 6:4
)

// Commands of the PCD.
const (
	cmdIdle       = 0x00
	cmdTransceive = 0x0C
	cmdAuth       = 0x0E
	cmdSoftReset  = 0x0F
)

// PICC commands and bits.
const (
	piccREQA     = 0x26
	piccWUPA     = 0x52
	piccAnticoll = 0x20
	piccSelect   = 0x70
	piccHalt     = 0x50
	piccRead     = 0x30
	piccWrite    = 0xA0
	piccKeyA     = 0x60
	piccKeyB     = 0x61
	sakComplete  = 0x20 // cascade bit clear: UID complete
)

const (
	irqRx          = 0x20
	irqIdle        = 0x10
	irqTimer       = 0x01
	irqErr         = 0x02
	errCRC         = 0x04
	errColl        = 0x08
	errProto       = 0x01
	bfStartSend    = 0x80
	txControlRF    = 0x03
	status2Crypto1 = 0x08
	authErrMask    = 0x1B
	errFatalMask   = 0x13
)

// KeyType selects the MIFARE key of an authentication.
type KeyType uint8

const (
	KeyA KeyType = piccKeyA
	KeyB KeyType = piccKeyB
)

// Card is a card that answered the selection.
type Card struct {
	// UID holds up to 10 bytes: 4 (MIFARE Classic), 7 (MIFARE Ultralight,
	// NTAG) or 10 (rare triple cascade).
	UID [10]byte
	// Length is the number of valid UID bytes (4, 7 or 10).
	Length uint8
	// SAK is the select acknowledge byte of the last cascade level.
	SAK byte
	// ATQA is the answer to request, 2 bytes.
	ATQA [2]byte
}

// UIDLength reports how many bytes of UID are valid.
func (c Card) UIDLength() int {
	if c.Length == 0 {
		return 4
	}
	return int(c.Length)
}

// UIDBytes returns the valid UID bytes.
func (c Card) UIDBytes() []byte { return c.UID[:c.UIDLength()] }

// Device is one Unit RFID2. It is not safe for concurrent use of the antenna:
// one card transaction at a time.
type Device struct {
	bus     I2C
	addr    uint16
	timeout time.Duration
	tx      [maxFrame + 2]byte // frames built by the caller
	rx      [maxFrame + 2]byte
	fifo    [maxFrame + 2]byte // [register, frame...] for the FIFO write
	reg     [2]byte            // register transactions, never a frame buffer
	one     [1]byte            // single register reads, never the rx buffer
}

// New returns a Device at the default address. Call Init before use.
func New(bus I2C) *Device {
	return &Device{bus: bus, addr: DefaultAddress, timeout: 50 * time.Millisecond}
}

// NewAt returns a Device at addr and runs Init.
func NewAt(bus I2C, addr uint16) (*Device, error) {
	d := New(bus)
	d.addr = addr
	if err := d.Init(); err != nil {
		return nil, err
	}
	return d, nil
}

// SetTimeout sets the wait limit of one transceive.
func (d *Device) SetTimeout(t time.Duration) {
	if t <= 0 {
		return
	}
	d.timeout = t
}

// Transceive is the low-level frame exchange: it sends frame with lastBits
// valid bits in the final byte, waits for an answer and returns up to rxLen
// bytes. CRC is not added and not checked.
//
// Use it for diagnostics or for commands this driver does not wrap.
func (d *Device) Transceive(frame []byte, lastBits byte, rxLen int) ([]byte, error) {
	return d.transceiveBits(frame, lastBits&0x07, rxLen)
}

// Register reads one register. It is the escape hatch for diagnostics and for
// commands this driver does not wrap yet.
func (d *Device) Register(reg byte) (byte, error) {
	return d.readReg(reg)
}

// SetRegister writes one register.
func (d *Device) SetRegister(reg, value byte) error {
	if err := d.writeReg(reg, value); err != nil {
		return ErrNotConnected
	}
	return nil
}

// Version returns the version register.
//
// Do not trust it for presence: MFRC522 reports 0x91/0x92, but WS1850S batches
// report other values (0x15 was measured on a real Unit RFID2). Init validates
// the chip with a register write + read-back instead.
func (d *Device) Version() (byte, error) {
	return d.readReg(regVersion)
}

// Init resets the chip, checks the version, sets the 13.56 MHz timers and turns
// the antenna on.
func (d *Device) Init() error {
	if d.bus == nil {
		return ErrNotConnected
	}
	// Soft reset, then wait for the PowerDown bit (CommandReg bit 4) to clear,
	// like the official PCD_Reset.
	if err := d.writeReg(regCommand, cmdSoftReset); err != nil {
		return ErrNotConnected
	}
	time.Sleep(50 * time.Millisecond)
	for i := 0; i < 20; i++ {
		v, err := d.readReg(regCommand)
		if err != nil {
			return ErrNotConnected
		}
		if v&0x10 == 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}

	// Timer exactly as the official C library: TAuto=1, TPrescaler 0x0A9 (169)
	// -> f_timer 40 kHz, reload 1000 -> 25 ms timeout.
	//
	// This write is also the presence test: some WS1850S batches do not report
	// 0x91/0x92 in VersionReg, so a register write + read-back is the reliable
	// check that the chip is there and the I2C mapping is right.
	if err := d.writeReg(regTMode, 0x80); err != nil {
		return ErrNotConnected
	}
	back, err := d.readReg(regTMode)
	if err != nil {
		return ErrNotConnected
	}
	if back != 0x80 {
		return ErrBadVersion
	}
	if err := d.writeReg(regTPrescaler, 0xA9); err != nil {
		return ErrNotConnected
	}
	if err := d.writeReg(regTReloadH, 0x03); err != nil {
		return ErrNotConnected
	}
	if err := d.writeReg(regTReloadL, 0xE8); err != nil {
		return ErrNotConnected
	}
	if err := d.writeReg(regTxASK, 0x40); err != nil {
		return ErrNotConnected
	}
	// CRC preset 0x6363, which is the ISO14443-A CRC_A preset.
	if err := d.writeReg(regMode, 0x3D); err != nil {
		return ErrNotConnected
	}
	return d.Antenna(true)
}

// Antenna turns the RF field on or off.
func (d *Device) Antenna(on bool) error {
	v, err := d.readReg(regTxControl)
	if err != nil {
		return ErrNotConnected
	}
	if on {
		v |= txControlRF
	} else {
		v &^= txControlRF
	}
	if err := d.writeReg(regTxControl, v); err != nil {
		return ErrNotConnected
	}
	if on {
		// The field needs time to settle before the first command; the
		// datasheet asks for a delay after AntennaOn.
		time.Sleep(20 * time.Millisecond)
	}
	return nil
}

// Reset stops the current command and clears the FIFO.
func (d *Device) Reset() error {
	if err := d.writeReg(regCommand, cmdIdle); err != nil {
		return ErrNotConnected
	}
	return d.writeReg(regFIFOLevel, 0x80)
}

// Halt puts the card in the field to halt.
func (d *Device) Halt() error {
	d.tx[0] = piccHalt
	d.tx[1] = 0x00
	crc := crcA(d.tx[:2])
	d.tx[2], d.tx[3] = byte(crc), byte(crc>>8)
	_, err := d.transceive(d.tx[:4], 0)
	return err
}

// Select sends REQA, runs the bit-level anticollision and selects one card.
//
// With several cards in the field it resolves the collisions like the official
// PICC_Select: it reads CollReg, keeps the prefix and chooses the bit that is 1,
// which makes the reader pick one card deterministically. Use SelectAll to
// enumerate every card, HALTing each one.
func (d *Device) Select() (Card, error) {
	return d.SelectContext(context.Background())
}

// SelectContext is Select with cancellation checks between the stages.
func (d *Device) SelectContext(ctx context.Context) (Card, error) {
	if err := ctx.Err(); err != nil {
		return Card{}, err
	}
	var c Card

	// REQA: 7 bits, no CRC. A card left in HALT only answers WUPA.
	reqa := [1]byte{piccREQA}
	resp, _, err := d.transceiveFrame(reqa[:], 7, 0, 2)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return c, ctxErr
		}
		d.tx[0] = piccWUPA
		resp, _, err = d.transceiveFrame(d.tx[:1], 7, 0, 2)
	}
	if err != nil || len(resp) < 2 {
		if err == nil {
			err = ErrNoCard
		}
		return c, err
	}
	c.ATQA[0], c.ATQA[1] = resp[0], resp[1]

	return d.selectLocked(&c, 0, nil)
}

// clearBits is below; selectLocked is the port of PICC_Select.
func (d *Device) selectLocked(c *Card, knownBits uint8, knownUID []byte) (Card, error) {
	// ValuesAfterColl = 1: bits received after a collision are cleared, so the
	// prefix stays valid while we resolve the collisions.
	if err := d.clearBits(regColl, 0x80); err != nil {
		return *c, ErrNotConnected
	}

	var buf [9]byte
	cascade := uint8(1)
	for {
		var sel, uidIndex uint8
		useCascadeTag := false
		switch cascade {
		case 1:
			sel, uidIndex = 0x93, 0
			useCascadeTag = knownBits > 0 && len(knownUID) > 4
		case 2:
			sel, uidIndex = 0x95, 3
			useCascadeTag = knownBits > 0 && len(knownUID) > 7
		case 3:
			sel, uidIndex = 0x97, 6
		default:
			return *c, ErrProtocol
		}

		cur := int(knownBits) - 8*int(uidIndex)
		if cur < 0 {
			cur = 0
		}
		buf[0] = sel
		idx := 2
		if useCascadeTag {
			buf[idx] = 0x88 // cascade tag
			idx++
		}
		toCopy := cur / 8
		if cur%8 != 0 {
			toCopy++
		}
		maxBytes := 4
		if useCascadeTag {
			maxBytes = 3
		}
		if toCopy > maxBytes {
			toCopy = maxBytes
		}
		for i := 0; i < toCopy; i++ {
			buf[idx] = knownUID[int(uidIndex)+i]
			idx++
		}
		if useCascadeTag {
			cur += 8
		}

		var resp []byte
		var rxLastBits byte
		selectDone := false
		for iter := 0; iter < 32 && !selectDone; iter++ {
			var txLastBits byte
			var used, respLen int
			if cur >= 32 {
				// SELECT: SEL + NVB 0x70 + 4 UID bytes + BCC + CRC_A.
				buf[1] = 0x70
				buf[6] = buf[2] ^ buf[3] ^ buf[4] ^ buf[5]
				crc := crcA(buf[:7])
				buf[7], buf[8] = byte(crc), byte(crc>>8)
				txLastBits = 0
				used = 9
				respLen = 3
			} else {
				// ANTICOLLISION: SEL + NVB(known bits) + the known prefix.
				// The answer lands in the free part of buf, which is how the
				// following SELECT gets the UID bytes (same as the official
				// driver, where responseBuffer points into buffer).
				txLastBits = byte(cur % 8)
				whole := cur / 8
				idx = 2 + whole
				buf[1] = byte(idx<<4) + txLastBits
				used = idx
				if txLastBits != 0 {
					used++
				}
				respLen = len(buf) - idx
			}
			rxAlign := txLastBits

			r, rlb, err := d.transceiveFrame(buf[:used], txLastBits, rxAlign, respLen)
			if err == ErrCollision {
				coll, cerr := d.readReg(regColl)
				if cerr != nil {
					return *c, ErrNotConnected
				}
				if coll&0x20 != 0 { // CollPosNotValid
					return *c, ErrCollision
				}
				pos := int(coll & 0x1F)
				if pos == 0 {
					pos = 32
				}
				if pos <= cur {
					return *c, ErrCollision // no progress
				}
				cur = pos
				bit := (cur - 1) % 8
				bi := 1 + cur/8
				if bit != 0 {
					bi++
				}
				if bi >= len(buf) {
					return *c, ErrProtocol
				}
				buf[bi] |= 1 << uint(bit)
				continue
			}
			if err != nil {
				return *c, err
			}
			resp, rxLastBits = r, rlb
			if cur >= 32 {
				selectDone = true
			} else {
				// The UID bits just received become the known prefix.
				copy(buf[idx:], r)
				cur = 32 // all 32 bits of this level are known now
			}
		}
		if !selectDone {
			return *c, ErrCollision
		}

		// Copy the UID bytes of this level (skipping the cascade tag).
		src, n := 2, 4
		if buf[2] == 0x88 {
			src, n = 3, 3
		}
		for i := 0; i < n; i++ {
			if int(uidIndex)+i < len(c.UID) {
				c.UID[int(uidIndex)+i] = buf[src+i]
			}
		}

		// The answer must be SAK + CRC_A, all bits valid.
		if len(resp) != 3 || rxLastBits != 0 {
			return *c, ErrProtocol
		}
		crc := crcA(resp[:1])
		if byte(crc) != resp[1] || byte(crc>>8) != resp[2] {
			return *c, ErrCRC
		}

		if resp[0]&0x04 != 0 { // cascade bit: more levels
			cascade++
			continue
		}
		c.SAK = resp[0]
		c.Length = 3*cascade + 1
		return *c, nil
	}
}

// SelectAll enumerates every card in the field.
//
// It selects one card, HALTs it (so it stops answering) and repeats. The cards
// must be able to answer one at a time, which is what the bit-level
// anticollision in Select guarantees.
func (d *Device) SelectAll(max int) ([]Card, error) {
	if max <= 0 {
		max = 8
	}
	var out []Card
	for i := 0; i < max; i++ {
		c, err := d.Select()
		if err != nil {
			if len(out) > 0 {
				return out, nil // no more cards
			}
			return nil, err
		}
		out = append(out, c)
		_ = d.Halt()
	}
	return out, nil
}

// StopCrypto1 releases the Crypto1 state after an authenticated operation, so
// the card can be selected again. The official driver does the same
// (PCD_StopCrypto1 clears the MFCrypto1On bit).
func (d *Device) StopCrypto1() error {
	if err := d.writeReg(regStatus2, 0x00); err != nil {
		return ErrNotConnected
	}
	return nil
}

// Authenticate runs the MIFARE authentication of block with key.
//
// The Crypto1 cipher runs inside the chip, so this is one command and no Go
// cryptography.
func (d *Device) Authenticate(block byte, keyType KeyType, key [6]byte, uid [4]byte) error {
	d.tx[0] = byte(keyType)
	d.tx[1] = block
	copy(d.tx[2:8], key[:])
	copy(d.tx[8:12], uid[:])

	if err := d.writeFIFO(d.tx[:12]); err != nil {
		return err
	}
	if err := d.writeReg(regCommand, cmdAuth); err != nil {
		return ErrNotConnected
	}
	if err := d.waitIRQ(irqIdle); err != nil {
		return err
	}
	errReg, err := d.readReg(regError)
	if err != nil {
		return ErrNotConnected
	}
	// The reference C driver treats these four flags as an authentication
	// failure: ProtocolErr, ParityErr, CollErr and BufferOvfl (mask 0x1B).
	if errReg&authErrMask != 0 {
		return ErrAuth
	}
	return nil
}

// ReadBlock reads one MIFARE block (16 bytes).
func (d *Device) ReadBlock(block byte) ([16]byte, error) {
	var out [16]byte
	d.tx[0] = piccRead
	d.tx[1] = block
	crc := crcA(d.tx[:2])
	d.tx[2], d.tx[3] = byte(crc), byte(crc>>8)

	rx, err := d.transceive(d.tx[:4], 18)
	if err != nil {
		return out, err
	}
	if len(rx) < 18 {
		return out, ErrFrame
	}
	got := uint16(rx[16]) | uint16(rx[17])<<8
	if crcA(rx[:16]) != got {
		return out, ErrCRC
	}
	copy(out[:], rx[:16])
	return out, nil
}

// WriteBlock writes one MIFARE block (16 bytes).
//
// MIFARE Classic writes in two phases: first the command with the block address
// and its CRC, wait for the 4-bit ACK, then the 16 data bytes with their CRC
// and a second ACK. Sending everything in one frame leaves the card waiting for
// the second phase and it stops answering.
func (d *Device) WriteBlock(block byte, data [16]byte) error {
	// Safety: block 0 is the manufacturer block (read only) and every fourth
	// block is a sector trailer. A wrong trailer can lock the card forever.
	if block == 0 || block%4 == 3 {
		return ErrBadBlock
	}
	// Phase 1: command + block + CRC.
	d.tx[0] = piccWrite
	d.tx[1] = block
	crc := crcA(d.tx[:2])
	d.tx[2], d.tx[3] = byte(crc), byte(crc>>8)
	if err := d.expectAck(d.tx[:4]); err != nil {
		return err
	}

	// Phase 2: the 16 data bytes + CRC.
	copy(d.tx[:16], data[:])
	crc = crcA(d.tx[:16])
	d.tx[16], d.tx[17] = byte(crc), byte(crc>>8)
	return d.expectAck(d.tx[:18])
}

// SetUID writes a new 4-byte UID on a magic card (Gen2), using the factory
// key A (FF FF FF FF FF FF) for sector 0.
//
// It does the whole block 0: reads the current block, replaces bytes 0..3,
// recomputes the BCC (UID0^UID1^UID2^UID3), keeps SAK, ATQA and the
// manufacturer bytes, writes the block in the two MIFARE phases and reads it
// back to verify. Forgetting the BCC makes the card fail anticollision, so this
// is the only supported way to change a UID.
//
// On a normal card block 0 is read-only: the card refuses and SetUID returns
// ErrNotMagic, with nothing written.
func (d *Device) SetUID(card Card, newUID [4]byte) error {
	return d.SetUIDWithKey(card, newUID, KeyA, [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF})
}

// SetUIDWithKey is SetUID with an explicit key for sector 0.
func (d *Device) SetUIDWithKey(card Card, newUID [4]byte, keyType KeyType, key [6]byte) error {
	if err := validUID(newUID); err != nil {
		return err
	}
	if card.UIDLength() != 4 {
		// A 7-byte UID card has a different block 0 layout.
		return ErrBadUID
	}

	var uid4 [4]byte
	copy(uid4[:], card.UID[:4])
	if err := d.Authenticate(0, keyType, key, uid4); err != nil {
		return err
	}
	original, err := d.ReadBlock(0)
	if err != nil {
		return err
	}

	blk := original
	copy(blk[0:4], newUID[:])
	blk[4] = newUID[0] ^ newUID[1] ^ newUID[2] ^ newUID[3]

	if err := d.writeBlockRaw(0, blk); err != nil {
		if err == ErrProtocol {
			return ErrNotMagic
		}
		return err
	}

	back, err := d.ReadBlock(0)
	if err != nil {
		return err
	}
	if back != blk {
		return ErrProtocol
	}
	return nil
}

// writeBlockRaw is the two-phase write without the block-0 guard.
func (d *Device) writeBlockRaw(block byte, data [16]byte) error {
	d.tx[0] = piccWrite
	d.tx[1] = block
	crc := crcA(d.tx[:2])
	d.tx[2], d.tx[3] = byte(crc), byte(crc>>8)
	if err := d.expectAck(d.tx[:4]); err != nil {
		return err
	}
	copy(d.tx[:16], data[:])
	crc = crcA(d.tx[:16])
	d.tx[16], d.tx[17] = byte(crc), byte(crc>>8)
	return d.expectAck(d.tx[:18])
}

// validUID rejects the two UIDs that brick a card: all bits zero and all bits
// one are not usable identities.
func validUID(uid [4]byte) error {
	allZero, allFF := true, true
	for _, b := range uid {
		if b != 0x00 {
			allZero = false
		}
		if b != 0xFF {
			allFF = false
		}
	}
	if allZero || allFF {
		return ErrBadUID
	}
	return nil
}

// expectAck sends frame and expects the 4-bit MIFARE ACK (0x0A).
func (d *Device) expectAck(frame []byte) error {
	rx, err := d.transceiveBits(frame, 0, 1)
	if err != nil {
		return err
	}
	if rx[0]&0x0F != 0x0A {
		return ErrProtocol
	}
	return nil
}

// transceive sends a frame with the CRC already appended, enables CRC on both
// directions when the expected answer is longer than one byte, and returns the
// answer.
func (d *Device) transceive(frame []byte, rxLen int) ([]byte, error) {
	return d.doTransceive(frame, 0, rxLen, rxLen > 1)
}

// transceiveBits sends frame, where txBits is the number of valid bits in the
// last byte (0 means 8), and expects rxLen bytes.
func (d *Device) transceiveBits(frame []byte, txBits byte, rxLen int) ([]byte, error) {
	return d.doTransceive(frame, txBits, rxLen, false)
}

func (d *Device) doTransceive(frame []byte, txBits byte, rxLen int, crcOn bool) ([]byte, error) {
	if len(frame) > maxFrame {
		return nil, ErrFrame
	}
	_ = crcOn // CRC goes in the frame; the chip does not add it (official does the same)

	// Same sequence as PCD_CommunicateWithPICC in the official driver.
	if err := d.writeReg(regCommand, cmdIdle); err != nil {
		return nil, ErrNotConnected
	}
	if err := d.writeReg(regComIrq, 0x7F); err != nil {
		return nil, ErrNotConnected
	}
	if err := d.setBits(regFIFOLevel, 0x80); err != nil { // FlushBuffer
		return nil, ErrNotConnected
	}
	if err := d.writeFIFO(frame); err != nil {
		return nil, err
	}
	if err := d.writeReg(regBitFraming, txBits&0x07); err != nil {
		return nil, ErrNotConnected
	}
	if err := d.writeReg(regCommand, cmdTransceive); err != nil {
		return nil, ErrNotConnected
	}
	if err := d.setBits(regBitFraming, bfStartSend); err != nil { // StartSend
		return nil, ErrNotConnected
	}

	if err := d.waitIRQ(irqRx | irqIdle); err != nil {
		return nil, err
	}
	if err := d.checkError(); err != nil {
		return nil, err
	}
	if rxLen == 0 {
		return nil, nil
	}
	return d.readFIFO(rxLen)
}

// clearBits is the read-modify-write used by PCD_ClearRegisterBitMask.
func (d *Device) clearBits(reg, mask byte) error {
	v, err := d.readReg(reg)
	if err != nil {
		return err
	}
	return d.writeReg(reg, v&^mask)
}

// setBits is the read-modify-write used by the official PCD_SetRegisterBitMask.
func (d *Device) setBits(reg, mask byte) error {
	v, err := d.readReg(reg)
	if err != nil {
		return err
	}
	return d.writeReg(reg, v|mask)
}

// transceiveFrame is the low-level exchange with full bit framing control:
// txBits valid bits in the last transmitted byte, rxAlign bits skipped at the
// start of the response. It returns the response bytes and the number of valid
// bits in the last received byte (ControlReg & 0x07).
//
// ErrCollision is returned when the chip reports a collision, so the caller can
// read CollReg and resolve it.
func (d *Device) transceiveFrame(frame []byte, txBits, rxAlign byte, rxMax int) ([]byte, byte, error) {
	if len(frame) > maxFrame {
		return nil, 0, ErrFrame
	}
	if err := d.writeReg(regCommand, cmdIdle); err != nil {
		return nil, 0, ErrNotConnected
	}
	if err := d.writeReg(regComIrq, 0x7F); err != nil {
		return nil, 0, ErrNotConnected
	}
	if err := d.setBits(regFIFOLevel, 0x80); err != nil {
		return nil, 0, ErrNotConnected
	}
	if err := d.writeFIFO(frame); err != nil {
		return nil, 0, err
	}
	if err := d.writeReg(regBitFraming, (rxAlign&0x07)<<4|(txBits&0x07)); err != nil {
		return nil, 0, ErrNotConnected
	}
	if err := d.writeReg(regCommand, cmdTransceive); err != nil {
		return nil, 0, ErrNotConnected
	}
	if err := d.setBits(regBitFraming, bfStartSend); err != nil {
		return nil, 0, ErrNotConnected
	}
	if err := d.waitIRQ(irqRx | irqIdle); err != nil {
		return nil, 0, err
	}

	// Collisions are not fatal here: the caller resolves them.
	v, err := d.readReg(regError)
	if err != nil {
		return nil, 0, ErrNotConnected
	}
	if v&errFatalMask != 0 {
		return nil, 0, ErrProtocol
	}
	collided := v&errColl != 0

	level, err := d.readReg(regFIFOLevel)
	if err != nil {
		return nil, 0, ErrNotConnected
	}
	n := int(level)
	if n > rxMax {
		n = rxMax
	}
	if n > len(d.rx) {
		n = len(d.rx)
	}
	if n > 0 {
		d.reg[0] = regFIFOData
		if err := d.bus.Tx(d.addr, d.reg[:1], nil); err != nil {
			return nil, 0, ErrNotConnected
		}
		if err := d.bus.Tx(d.addr, nil, d.rx[:n]); err != nil {
			return nil, 0, ErrNotConnected
		}
	}
	ctl, err := d.readReg(regControl)
	if err != nil {
		return nil, 0, ErrNotConnected
	}
	if collided {
		return d.rx[:n], ctl & 0x07, ErrCollision
	}
	return d.rx[:n], ctl & 0x07, nil
}

// waitIRQ waits for any of the mask bits in ComIrqReg.
func (d *Device) waitIRQ(mask byte) error {
	deadline := time.Now().Add(d.timeout)
	for {
		v, err := d.readReg(regComIrq)
		if err != nil {
			return ErrNotConnected
		}
		if v&mask != 0 {
			return nil
		}
		// Only the timer means "nothing arrived". The official driver ignores
		// ErrIRq here and reads ErrorReg after the wait; aborting on ErrIRq
		// returned a timeout before the card could answer.
		if v&irqTimer != 0 {
			return ErrTimeout
		}
		if time.Now().After(deadline) {
			return ErrTimeout
		}
		time.Sleep(200 * time.Microsecond)
	}
}

// checkError maps ErrorReg to a sentinel.
func (d *Device) checkError() error {
	v, err := d.readReg(regError)
	if err != nil {
		return ErrNotConnected
	}
	// Same mask as the official driver: BufferOvfl, ParityErr, ProtocolErr.
	// CRCErr is not used here because CRC_A is verified in software.
	if v&errFatalMask != 0 {
		return ErrProtocol
	}
	if v&errColl != 0 {
		return ErrCollision
	}
	return nil
}

func (d *Device) writeFIFO(b []byte) error {
	if len(b) == 0 || len(b) > maxFrame {
		return ErrFrame
	}
	d.fifo[0] = regFIFOData
	copy(d.fifo[1:], b)
	if err := d.bus.Tx(d.addr, d.fifo[:1+len(b)], nil); err != nil {
		return ErrNotConnected
	}
	return nil
}

func (d *Device) readFIFO(n int) ([]byte, error) {
	if n <= 0 || n > maxFrame {
		return nil, ErrFrame
	}
	level, err := d.readReg(regFIFOLevel)
	if err != nil {
		return nil, ErrNotConnected
	}
	if int(level) < n {
		return nil, ErrFrame
	}
	d.reg[0] = regFIFOData
	if err := d.bus.Tx(d.addr, d.reg[:1], nil); err != nil {
		return nil, ErrNotConnected
	}
	if err := d.bus.Tx(d.addr, nil, d.rx[:n]); err != nil {
		return nil, ErrNotConnected
	}
	return d.rx[:n], nil
}

func (d *Device) readReg(reg byte) (byte, error) {
	// Two transactions, with a STOP in between, like the C library
	// (endTransmission then requestFrom). The unit's I2C bridge does not accept
	// a repeated start for the register read.
	d.reg[0] = reg
	if err := d.bus.Tx(d.addr, d.reg[:1], nil); err != nil {
		return 0, err
	}
	if err := d.bus.Tx(d.addr, nil, d.one[:]); err != nil {
		return 0, err
	}
	return d.one[0], nil
}

func (d *Device) writeReg(reg, value byte) error {
	d.reg[0] = reg
	d.reg[1] = value
	return d.bus.Tx(d.addr, d.reg[:2], nil)
}

// crcA is the ISO/IEC 14443-A CRC: polynomial 0x8408 reflected, init 0x6363.
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
