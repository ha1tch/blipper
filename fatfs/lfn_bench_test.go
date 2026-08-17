package fatfs

import "testing"

// The cost of long-name support is paid on directory load, which
// happens once per Open: with the option on, every entry carrying
// attribute 0x0F is buffered and each short entry that follows a
// run pays a checksum validation and a UTF-16 decode.
//
// These benchmarks measure that, so the decision to leave the
// option off by default rests on a number rather than an
// intuition.

func benchOpen(b *testing.B, image string, opts ...Option) {
	data := loadImageBytes(b, image)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		vol, err := OpenImage(&memImage{data: data}, opts...)
		if err != nil {
			b.Fatalf("OpenImage: %v", err)
		}
		_ = vol.List()
	}
}

func BenchmarkOpenShortNamesOnly(b *testing.B) {
	benchOpen(b, "lfn32.img.gz")
}

func BenchmarkOpenWithLongNames(b *testing.B) {
	benchOpen(b, "lfn32.img.gz", WithLongNames(true))
}

func BenchmarkOpenPlainImageShortNames(b *testing.B) {
	benchOpen(b, "fat32.img.gz")
}

func BenchmarkOpenPlainImageWithLongNames(b *testing.B) {
	benchOpen(b, "fat32.img.gz", WithLongNames(true))
}
