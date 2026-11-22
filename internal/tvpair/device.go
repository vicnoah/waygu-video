package tvpair

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"unicode"
)

const peerHeader = "X-Waygu-Pair-Device"

type Device struct {
	ID   string
	Name string
	Kind string
}

type localDeviceKey struct{}
type peerDeviceKey struct{}

func WithDevice(ctx context.Context, device Device) context.Context {
	return context.WithValue(ctx, localDeviceKey{}, device.normalized())
}

func deviceOn(ctx context.Context) Device {
	device, _ := ctx.Value(localDeviceKey{}).(Device)
	return device
}

func withPeer(ctx context.Context, device Device) context.Context {
	if device.ID == "" {
		return ctx
	}
	return context.WithValue(ctx, peerDeviceKey{}, device)
}

func Peer(ctx context.Context) Device {
	device, _ := ctx.Value(peerDeviceKey{}).(Device)
	return device
}

func (d Device) normalized() Device {
	if d.ID == "" || len(d.ID) > 64 {
		return Device{}
	}
	for i := range len(d.ID) {
		ch := d.ID[i]
		if (ch < 'A' || ch > 'Z') && (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '-' && ch != '_' {
			return Device{}
		}
	}
	d.Name = strings.Join(strings.FieldsFunc(d.Name, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }), " ")
	if name := []rune(d.Name); len(name) > 80 {
		d.Name = string(name[:80])
	}
	switch d.Kind {
	case "phone", "tablet", "desktop", "tv":
	default:
		d.Kind = ""
	}
	return d
}

func encodeDevice(device Device) string {
	device = device.normalized()
	if device.ID == "" {
		return ""
	}
	raw, err := json.Marshal(device)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func parseDevice(value string) Device {
	if value == "" || len(value) > 1024 {
		return Device{}
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return Device{}
	}
	var device Device
	if json.Unmarshal(raw, &device) != nil {
		return Device{}
	}
	return device.normalized()
}
