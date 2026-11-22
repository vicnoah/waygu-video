package audiomarker

import (
	"context"
	"time"
)

const minimumMatch = 6 * time.Second
const minFrames = int(minimumMatch / step)

type Range struct{ Start, End time.Duration }

type frameRange struct{ start, end int }

func (r frameRange) length() int { return r.end - r.start }

// Shared locates one recurring, changing pattern in the current window that
// independently matches both same-season episodes.
func Shared(ctx context.Context, current, first, second Fingerprint) (Range, bool) {
	left := longest(ctx, current, first)
	right := longest(ctx, current, second)
	overlap := frameRange{max(left.start, right.start), min(left.end, right.end)}
	if overlap.length() < minFrames || ctx.Err() != nil {
		return Range{}, false
	}
	return Range{time.Duration(overlap.start) * step, time.Duration(overlap.end) * step}, true
}

func similarity(a, b signature) float32 {
	if !a.valid || !b.valid {
		return 0
	}
	var sum float32
	for i, x := range a.energy {
		sum += x * b.energy[i]
	}
	return sum
}

func longest(ctx context.Context, source, peer Fingerprint) frameRange {
	var best frameRange
	for offset := minFrames - len(source); offset <= len(peer)-minFrames; offset++ {
		if ctx.Err() != nil {
			return frameRange{}
		}
		begin, end := max(0, -offset), min(len(source), len(peer)-offset)
		start, matched, gaps, changes := begin, 0, 0, 0
		for i := begin; i < end; i++ {
			if similarity(source[i], peer[i+offset]) < 0.94 {
				gaps++
				if gaps > 2 {
					start, matched, changes = i+1, 0, 0
				}
				continue
			}
			if i >= start+4 && similarity(source[i], source[i-4]) < 0.88 {
				changes++
			}
			matched++
			gaps = 0
			length := i + 1 - start
			if length >= minFrames && length > best.length() && matched*10 >= length*9 && changes*6 >= length {
				best = frameRange{start, i + 1}
			}
		}
	}
	return best
}
