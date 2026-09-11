package speech

import (
	"bytes"
	"encoding/binary"
)

const (
	pcmSampleRate = 24000 // OpenRouter returns pcm as 24 kHz mono 16-bit samples
	pcmChannels   = 1
	pcmBitDepth   = 16
	wavHeaderLen  = 36
	fmtChunkLen   = 16
	pcmFormatTag  = 1
)

// wavFromPCM16 wraps raw little-endian 16-bit PCM samples in a WAV container.
func wavFromPCM16(pcm []byte, rate, channels int) []byte {
	var b bytes.Buffer

	dataLen := uint32(len(pcm))                           //nolint:gosec // audio clips are far below 4 GiB
	byteRate := uint32(rate * channels * pcmBitDepth / 8) //nolint:gosec // small positive constants
	blockAlign := uint16(channels * pcmBitDepth / 8)      //nolint:gosec // small positive constants

	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, wavHeaderLen+dataLen)
	b.WriteString("WAVE")
	b.WriteString("fmt ")
	_ = binary.Write(&b, binary.LittleEndian, uint32(fmtChunkLen))
	_ = binary.Write(&b, binary.LittleEndian, uint16(pcmFormatTag))
	_ = binary.Write(&b, binary.LittleEndian, uint16(channels)) //nolint:gosec // 1 or 2
	_ = binary.Write(&b, binary.LittleEndian, uint32(rate))     //nolint:gosec // small rate
	_ = binary.Write(&b, binary.LittleEndian, byteRate)
	_ = binary.Write(&b, binary.LittleEndian, blockAlign)
	_ = binary.Write(&b, binary.LittleEndian, uint16(pcmBitDepth))
	b.WriteString("data")
	_ = binary.Write(&b, binary.LittleEndian, dataLen)
	b.Write(pcm)

	return b.Bytes()
}
