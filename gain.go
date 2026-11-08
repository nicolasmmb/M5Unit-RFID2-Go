package rfid2

// Gain is the receiver gain of the analog front end (RxGain[2:0] of RFCfgReg).
// More gain means more range and more sensitivity to noise.
type Gain uint8

const (
	Gain18dB Gain = iota // 18 dB, the reset value
	Gain23dB
	Gain28dB
	Gain33dB
	Gain38dB
	Gain43dB
	Gain48dB // maximum
)

// DB returns the gain in decibels.
func (g Gain) DB() uint8 { return 18 + 5*uint8(g) }

// String returns "Gain38dB" style names for the logs.
func (g Gain) String() string { return gainNames[g] }

var gainNames = [7]string{
	"Gain18dB", "Gain23dB", "Gain28dB", "Gain33dB", "Gain38dB", "Gain43dB", "Gain48dB",
}

// SetAntennaGain writes RxGain[2:0] of RFCfgReg (0x26), keeping the other bits,
// like PCD_SetAntennaGain in the official driver.
func (d *Device) SetAntennaGain(g Gain) error {
	if g > Gain48dB {
		g = Gain48dB
	}
	v, err := d.readReg(regRFCfg)
	if err != nil {
		return ErrNotConnected
	}
	if err := d.writeReg(regRFCfg, v&0x8F|byte(g)<<4); err != nil {
		return ErrNotConnected
	}
	return nil
}

// GetAntennaGain reads RxGain[2:0] back.
func (d *Device) GetAntennaGain() (Gain, error) {
	v, err := d.readReg(regRFCfg)
	if err != nil {
		return 0, ErrNotConnected
	}
	return Gain((v & 0x70) >> 4), nil
}
