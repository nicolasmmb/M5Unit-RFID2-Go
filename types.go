package rfid2

// Type is the card family, derived from the SAK like PICC_GetType in the
// official driver.
type Type uint8

// The numeric values match PICC_Type in the official driver, so logs and
// fixtures from both sides can be compared directly.
const (
	TypeUnknown     Type = 0
	TypeISO14443_4  Type = 1
	TypeISO18092    Type = 2
	TypeMifareMini  Type = 3
	TypeMifare1K    Type = 4
	TypeMifare4K    Type = 5
	TypeMifareUL    Type = 6
	TypeMifarePlus  Type = 7
	TypeTNP3XXX     Type = 8
	TypeNotComplete Type = 255
)

// String returns the name used in the logs.
func (t Type) String() string {
	switch t {
	case TypeNotComplete:
		return "not complete (cascade)"
	case TypeMifareMini:
		return "MIFARE Mini"
	case TypeMifare1K:
		return "MIFARE Classic 1K"
	case TypeMifare4K:
		return "MIFARE Classic 4K"
	case TypeMifareUL:
		return "MIFARE Ultralight"
	case TypeMifarePlus:
		return "MIFARE Plus"
	case TypeTNP3XXX:
		return "TNP3xxx"
	case TypeISO14443_4:
		return "ISO/IEC 14443-4"
	case TypeISO18092:
		return "ISO/IEC 18092 (NFC)"
	default:
		return "unknown"
	}
}

// Type returns the card family from the SAK byte of the selection.
//
// The same table as PICC_GetType in the official driver. Note that SAK 0x00 is
// reported as MIFARE Ultralight: MIFARE Classic clones that answer 0x00 exist,
// so also look at the UID length when it matters.
func (c Card) Type() Type {
	if c.SAK&0x04 != 0 {
		return TypeNotComplete
	}
	switch c.SAK {
	case 0x09:
		return TypeMifareMini
	case 0x08:
		return TypeMifare1K
	case 0x18:
		return TypeMifare4K
	case 0x00:
		return TypeMifareUL
	case 0x10, 0x11:
		return TypeMifarePlus
	case 0x01:
		return TypeTNP3XXX
	case 0x20:
		return TypeISO14443_4
	case 0x40:
		return TypeISO18092
	}
	return TypeUnknown
}
