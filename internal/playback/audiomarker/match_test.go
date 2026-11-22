package audiomarker

import (
	"testing"
	"time"
)

func TestSharedRequiresIndependentChangingMatches(t *testing.T) {
	const frames = 72
	theme := make(Fingerprint, frames)
	for i := range theme {
		theme[i].valid = true
		theme[i].energy[(i/3)%15] = 1
	}
	advance := func(offset int, audio Fingerprint) Fingerprint {
		return append(append(make(Fingerprint, offset), audio...), make(Fingerprint, 10)...)
	}
	source := advance(12, theme)
	result, ok := Shared(t.Context(), source, advance(21, theme), advance(3, theme))
	if !ok || result.Start != 1500*time.Millisecond || result.End != 10500*time.Millisecond {
		t.Fatalf("shifted theme: %+v, %v", result, ok)
	}
	steady := make(Fingerprint, frames)
	for i := range steady {
		steady[i] = theme[0]
	}
	if result, ok := Shared(t.Context(), steady, steady, steady); ok {
		t.Fatalf("a steady tone was identified as an intro: %+v", result)
	}
	if result, ok := Shared(t.Context(), source, advance(21, theme), steady); ok {
		t.Fatalf("only one independent comparison matched: %+v", result)
	}
}
