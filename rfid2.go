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
)

// KeyType selects the MIFARE key of an authentication.
type KeyType uint8

const (
	KeyA KeyType = piccKeyA
	KeyB KeyType = piccKeyB
)

// Card is a card that answered the selection.
type Card struct {
	// UID is the serial number, 4 bytes for MIFARE Classic.
	UID [4]byte
	// SAK is the select acknowledge byte.
	SAK byte
	// ATQA is the answer to request, 2 bytes.
	ATQA [2]byte
}

// UIDLength reports how many bytes of UID are valid.
func (c Card) UIDLength() int { return 4 }

// Device is one Unit RFID2. It is not safe for concurrent use of the antenna:
// one card transaction at a time.
type Device struct {
	bus     I2C
	addr    uint16
	timeout time.Duration
	tx      [maxFrame + 2]byte // frames built by the caller
	rx      [maxFrame + 2]byte
	fifo    [maxFrame + 2]byte // [register, frame...] for the FIFO write
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
	// Soft reset and wait for the reset phase to finish.
	if err := d.writeReg(regCommand, cmdSoftReset); err != nil {
		return ErrNotConnected
	}
	time.Sleep(50 * time.Millisecond)

	// Timer: TAuto=1, TPrescalerEven=1, TPrescaler 169 -> 40.8 kHz.
	//
	// This write is also the presence test: some WS1850S batches do not report
	// 0x91/0x92 in VersionReg, so a register write + read-back is the reliable
	// check that the chip is there and the I2C mapping is right.
	if err := d.writeReg(regTMode, 0x8D); err != nil {
		return ErrNotConnected
	}
	back, err := d.readReg(regTMode)
	if err != nil {
		return ErrNotConnected
	}
	if back != 0x8D {
		return ErrBadVersion
	}
	if err := d.writeReg(regTPrescaler, 0x3E); err != nil {
		return ErrNotConnected
	}
	// Reload 30: about 25 ms, used as the transceive timeout.
	if err := d.writeReg(regTReloadH, 0x00); err != nil {
		return ErrNotConnected
	}
	if err := d.writeReg(regTReloadL, 30); err != nil {
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

// Select sends REQA, runs the anticollision and selects the card.
func (d *Device) Select() (Card, error) {
	return d.SelectContext(context.Background())
}

// SelectContext is Select with a cancellation check between the stages.
func (d *Device) SelectContext(ctx context.Context) (Card, error) {
	var c Card

	// REQA: 7 bits, no CRC.
	if err := ctx.Err(); err != nil {
		return c, err
	}
	reqa := [1]byte{piccREQA}
	resp, err := d.transceiveBits(reqa[:], 7, 2)
	if err != nil {
		return c, err
	}
	c.ATQA[0], c.ATQA[1] = resp[0], resp[1]

	// Anticollision level 1: 0x93 0x20, 5 bytes answer (4 UID + BCC).
	if err := ctx.Err(); err != nil {
		return c, err
	}
	anti := [2]byte{0x93, piccAnticoll}
	uid, err := d.transceive(anti[:], 5)
	if err != nil {
		return c, err
	}
	var bcc byte
	for i := 0; i < 4; i++ {
		c.UID[i] = uid[i]
		bcc ^= uid[i]
	}
	if bcc != uid[4] {
		return c, ErrProtocol
	}

	// Select: 0x93 0x70 + 5 bytes + CRC_A, answer is SAK + CRC.
	if err := ctx.Err(); err != nil {
		return c, err
	}
	d.tx[0], d.tx[1] = 0x93, piccSelect
	for i := 0; i < 5; i++ {
		d.tx[2+i] = uid[i]
	}
	crc := crcA(d.tx[:7])
	d.tx[7], d.tx[8] = byte(crc), byte(crc>>8)
	sak, err := d.transceive(d.tx[:9], 3)
	if err != nil {
		return c, err
	}
	c.SAK = sak[0]
	return c, nil
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

// WriteBlock writes one MIFARE block (16 bytes) and waits for the ACK.
func (d *Device) WriteBlock(block byte, data [16]byte) error {
	d.tx[0] = piccWrite
	d.tx[1] = block
	copy(d.tx[2:18], data[:])
	crc := crcA(d.tx[:18])
	d.tx[18], d.tx[19] = byte(crc), byte(crc>>8)

	// The card answers with 4 bits (0x0A) and no CRC.
	rx, err := d.transceiveBits(d.tx[:20], 0, 1)
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
	if err := d.writeFIFO(frame); err != nil {
		return nil, err
	}
	if crcOn {
		if err := d.writeReg(regTxMode, 0x80); err != nil { // TxCRCEn
			return nil, ErrNotConnected
		}
		if err := d.writeReg(regRxMode, 0x80); err != nil { // RxCRCEn
			return nil, ErrNotConnected
		}
	} else {
		if err := d.writeReg(regTxMode, 0x00); err != nil {
			return nil, ErrNotConnected
		}
		if err := d.writeReg(regRxMode, 0x00); err != nil {
			return nil, ErrNotConnected
		}
	}
	// Clear the IRQ flags and start the transceive.
	if err := d.writeReg(regComIrq, 0x7F); err != nil {
		return nil, ErrNotConnected
	}
	if err := d.writeReg(regBitFraming, bfStartSend|txBits); err != nil {
		return nil, ErrNotConnected
	}
	if err := d.writeReg(regCommand, cmdTransceive); err != nil {
		return nil, ErrNotConnected
	}
	if err := d.waitIRQ(irqRx); err != nil {
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
		if v&irqErr != 0 || v&irqTimer != 0 {
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
	switch {
	case v&errCRC != 0:
		return ErrCRC
	case v&errColl != 0:
		return ErrCollision
	case v&errProto != 0:
		return ErrProtocol
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
	d.tx[0] = regFIFOData
	if err := d.bus.Tx(d.addr, d.tx[:1], nil); err != nil {
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
	d.tx[0] = reg
	if err := d.bus.Tx(d.addr, d.tx[:1], nil); err != nil {
		return 0, err
	}
	if err := d.bus.Tx(d.addr, nil, d.rx[:1]); err != nil {
		return 0, err
	}
	return d.rx[0], nil
}

func (d *Device) writeReg(reg, value byte) error {
	d.tx[0] = reg
	d.tx[1] = value
	return d.bus.Tx(d.addr, d.tx[:2], nil)
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
