package config

import (
	"bytes"
	"encoding/binary"
	"slices"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// textEncoding is how a text file sshush appends to is stored on disk. It
// matters for PowerShell profiles: Windows PowerShell 5.1 writes UTF-16LE with
// a byte-order mark whenever a file is made with ">" or Out-File, and UTF-8
// appended to one of those leaves a file PowerShell cannot parse — an error in
// every new window, from a file the user did not knowingly change.
type textEncoding int

const (
	encodingUTF8    textEncoding = iota // no byte-order mark
	encodingUTF8BOM                     // EF BB BF
	encodingUTF16LE                     // FF FE
	encodingUTF16BE                     // FE FF
)

var (
	bomUTF8    = []byte{0xEF, 0xBB, 0xBF}
	bomUTF16LE = []byte{0xFF, 0xFE}
	bomUTF16BE = []byte{0xFE, 0xFF}
)

// decodeTextFile returns the text in content and the encoding it was read as,
// going by the byte-order mark, which is not part of the text returned. ok is
// false when content is not cleanly that encoding, in which case nothing can be
// appended safely: an odd number of bytes or an unpaired surrogate in UTF-16,
// an invalid sequence in UTF-8, or a NUL anywhere. No text file has a NUL, and
// finding one is how UTF-16 without a mark (valid UTF-8, byte for byte, when
// the text is ASCII) and UTF-32 (whose little-endian mark begins FF FE) are
// told from what they would otherwise pass for.
func decodeTextFile(content []byte) (text string, enc textEncoding, ok bool) {
	var order binary.ByteOrder
	switch {
	case bytes.HasPrefix(content, bomUTF8):
		enc, content = encodingUTF8BOM, content[len(bomUTF8):]
	case bytes.HasPrefix(content, bomUTF16LE):
		enc, content, order = encodingUTF16LE, content[len(bomUTF16LE):], binary.LittleEndian
	case bytes.HasPrefix(content, bomUTF16BE):
		enc, content, order = encodingUTF16BE, content[len(bomUTF16BE):], binary.BigEndian
	}
	if order == nil {
		if !utf8.Valid(content) || bytes.IndexByte(content, 0) >= 0 {
			return "", enc, false
		}
		return string(content), enc, true
	}

	if len(content)%2 != 0 {
		return "", enc, false
	}
	units := make([]uint16, len(content)/2)
	for i := range units {
		units[i] = order.Uint16(content[2*i:])
	}
	runes := utf16.Decode(units)
	// Decode puts U+FFFD where a surrogate has no partner, so text that does
	// not encode back to the same units was not valid UTF-16.
	if !slices.Equal(utf16.Encode(runes), units) {
		return "", enc, false
	}
	text = string(runes)
	if strings.ContainsRune(text, 0) {
		return "", enc, false
	}
	return text, enc, true
}

// encodeText returns text as bytes to append to a file in enc: no byte-order
// mark, since that belongs only at the start of the file.
func encodeText(text string, enc textEncoding) []byte {
	var order binary.ByteOrder
	switch enc {
	case encodingUTF16LE:
		order = binary.LittleEndian
	case encodingUTF16BE:
		order = binary.BigEndian
	default:
		return []byte(text)
	}
	units := utf16.Encode([]rune(text))
	out := make([]byte, 2*len(units))
	for i, u := range units {
		order.PutUint16(out[2*i:], u)
	}
	return out
}
