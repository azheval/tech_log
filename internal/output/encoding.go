package output

func withUTF8BOM(data []byte) []byte {
	out := make([]byte, 0, len(data)+3)
	out = append(out, 0xEF, 0xBB, 0xBF)
	out = append(out, data...)
	return out
}
