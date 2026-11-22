// Package audiomarker identifies recurring audio without retaining recordings.
package audiomarker

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"math/bits"
	"math/cmplx"
	"time"
)

const (
	sampleRate  = 16_000
	fftSize     = 1024
	step        = 125 * time.Millisecond
	stepSamples = sampleRate / 8
	bands       = 20
)

// Fingerprint is the sequence of normalized frequency bands in an audio window.
type Fingerprint []signature

type signature struct {
	energy [bands]float32
	valid  bool
}

var errPCM = errors.New("expected mono 16 kHz PCM WAV")

// ReadWAV analyzes a bounded portion of mono PCM16. A streaming WAV may
// advertise an unknown data length, so the caller's span always bounds reads.
func ReadWAV(ctx context.Context, source io.Reader, span time.Duration) (Fingerprint, error) {
	reader, err := pcm(source)
	if err != nil {
		return nil, err
	}
	count := int(span / step)
	result := make(Fingerprint, 0, count)
	var block [stepSamples * 2]byte
	var samples [fftSize]complex128
	for range count {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if _, err := io.ReadFull(reader, block[:]); err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				break
			}
			return nil, err
		}
		offset := (stepSamples - fftSize) / 2
		var amplitude float64
		for i := range samples {
			from := (offset + i) * 2
			value := float64(int16(binary.LittleEndian.Uint16(block[from:]))) / 32768
			samples[i] = complex(value*window[i], 0)
			amplitude += value * value
		}
		result = append(result, describe(&samples, amplitude))
	}
	return result, nil
}

func pcm(source io.Reader) (io.Reader, error) {
	var header [12]byte
	if _, err := io.ReadFull(source, header[:]); err != nil || string(header[:4]) != "RIFF" || string(header[8:]) != "WAVE" {
		return nil, errPCM
	}
	var formatOK bool
	var consumed uint32
	for consumed < 128<<10 {
		var chunk [8]byte
		if _, err := io.ReadFull(source, chunk[:]); err != nil {
			return nil, err
		}
		size := binary.LittleEndian.Uint32(chunk[4:])
		switch string(chunk[:4]) {
		case "data":
			if !formatOK {
				return nil, errPCM
			}
			return io.LimitReader(source, int64(size)), nil
		case "fmt ":
			if size < 16 || size > 128 {
				return nil, errPCM
			}
			var format [16]byte
			if _, err := io.ReadFull(source, format[:]); err != nil {
				return nil, err
			}
			formatOK = binary.LittleEndian.Uint16(format[:]) == 1 &&
				binary.LittleEndian.Uint16(format[2:]) == 1 &&
				binary.LittleEndian.Uint32(format[4:]) == sampleRate &&
				binary.LittleEndian.Uint16(format[14:]) == 16
			size -= 16
			consumed += 16
		}
		if size > 128<<10 {
			return nil, errPCM
		}
		padded := size + size%2
		consumed += padded + 8
		if _, err := io.CopyN(io.Discard, source, int64(padded)); err != nil {
			return nil, err
		}
	}
	return nil, errPCM
}

var window = func() [fftSize]float64 {
	var result [fftSize]float64
	for i := range result {
		result[i] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/(fftSize-1))
	}
	return result
}()

var bandByBin = func() [fftSize / 2]int {
	var result [fftSize / 2]int
	for i := range result {
		hz := float64(i) * sampleRate / fftSize
		result[i] = -1
		if hz >= 94 && hz < 6000 {
			result[i] = min(bands-1, int(math.Log2(hz/94)*3))
		}
	}
	return result
}()

var twiddle = func() [fftSize / 2]complex128 {
	var result [fftSize / 2]complex128
	for i := range result {
		sine, cosine := math.Sincos(-2 * math.Pi * float64(i) / fftSize)
		result[i] = complex(cosine, sine)
	}
	return result
}()

func describe(samples *[fftSize]complex128, amplitude float64) signature {
	if amplitude < fftSize*0.00001 {
		return signature{}
	}
	for i := range samples {
		reversed := int(bits.Reverse16(uint16(i)) >> 6)
		if reversed > i {
			samples[i], samples[reversed] = samples[reversed], samples[i]
		}
	}
	for width := 2; width <= fftSize; width *= 2 {
		half := width / 2
		for offset := 0; offset < fftSize; offset += width {
			for i := range half {
				left, right := offset+i, offset+i+half
				even, odd := samples[left], samples[right]*twiddle[i*fftSize/width]
				samples[left], samples[right] = even+odd, even-odd
			}
		}
	}
	var power [bands]float64
	for bin, band := range bandByBin {
		if band >= 0 {
			magnitude := cmplx.Abs(samples[bin])
			power[band] += magnitude * magnitude
		}
	}
	var peak float64
	for _, value := range power {
		peak = max(peak, value)
	}
	if peak == 0 {
		return signature{}
	}
	var result signature
	var norm float64
	for i, value := range power {
		weight := math.Log1p(value / peak * 48)
		result.energy[i] = float32(weight)
		norm += weight * weight
	}
	scale := float32(1 / math.Sqrt(norm))
	for i := range result.energy {
		result.energy[i] *= scale
	}
	result.valid = true
	return result
}
