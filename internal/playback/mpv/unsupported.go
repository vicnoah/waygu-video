//go:build !((linux && !android) || (darwin && !ios) || windows)

package mpv

import (
	"image"
	"time"

	"waygu-video/internal/danmaku"
)

type backend struct{}

func open() (*Player, error)                           { return nil, ErrUnavailable }
func available() bool                                  { return false }
func (*backend) close()                                {}
func (*backend) command(...string) error               { return ErrUnavailable }
func (*backend) property(string, string) error         { return ErrUnavailable }
func (*backend) seek(time.Duration) error              { return ErrUnavailable }
func (*backend) poll() Event                           { return Event{} }
func (*backend) stats() Stats                          { return Stats{} }
func (*backend) status() Status                        { return Status{} }
func (*backend) tracks() []Track                       { return nil }
func (*backend) intProperty(string) int64              { return 0 }
func (*backend) stringProperty(string) string          { return "" }
func (*backend) frame(*image.RGBA, bool) (bool, error) { return false, ErrUnavailable }
func (*backend) danmaku(*danmaku.Track)                {}
