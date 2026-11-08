package rfid2

import "testing"

func benchDevice(b *testing.B) *Device {
	d := New(&silentChip{})
	d.SetTimeout(5_000_000)
	return d
}

// BenchmarkCRC is the ISO14443-A CRC of one 4-byte frame.
func BenchmarkCRC(b *testing.B) {
	frame := [4]byte{piccRead, 0x04, 0x00, 0x00}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = crcA(frame[:])
	}
}

// BenchmarkReadFIFO is the register plus FIFO read of one answer.
func BenchmarkReadFIFO(b *testing.B) {
	d := benchDevice(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = d.readFIFO(18)
	}
}

// BenchmarkTransceive measures the command sequence without the card wait.
func BenchmarkTransceive(b *testing.B) {
	d := benchDevice(b)
	frame := []byte{piccREQA}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = d.transceiveBits(frame, 7, 2)
	}
}
