package main

import (
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"waygu-video/internal/tvpair"
)

const pairedDevicesKey = "paired-devices-v1"
const maxPairedDevices = 20

type pairedDevice struct {
	Device tvpair.Device
	SeenAt time.Time
}

func readPairedDevices(storage core.Storage) []pairedDevice {
	raw, ok := storage.GetItem(pairedDevicesKey)
	var values []pairedDevice
	if !ok || json.Unmarshal([]byte(raw), &values) != nil {
		return nil
	}
	values = slices.DeleteFunc(values, func(row pairedDevice) bool { return row.Device.ID == "" || row.SeenAt.IsZero() })
	return values[:min(len(values), maxPairedDevices)]
}

func (a *application) pairingDevice(cx *core.Scope) tvpair.Device {
	kind := "desktop"
	switch appLayoutIdiom(cx) {
	case core.IdiomPhone:
		kind = "phone"
	case core.IdiomTablet:
		kind = "tablet"
	case core.IdiomTV:
		kind = "tv"
	}
	name := strings.TrimSpace(cx.Env().DeviceName())
	if name == "" {
		name = strings.TrimSpace(cx.Env().DeviceModel())
	}
	return tvpair.Device{ID: a.device.ID, Name: name, Kind: kind}
}

func (a *application) rememberPairedDevice(device tvpair.Device) {
	if device.ID == "" || device.ID == a.device.ID {
		return
	}
	devices := slices.DeleteFunc(slices.Clone(a.pairedDevices.GetUntracked()), func(row pairedDevice) bool { return row.Device.ID == device.ID })
	devices = append([]pairedDevice{{Device: device, SeenAt: a.scope.WallTime().UTC()}}, devices...)
	_ = storeSettings(a.storage, pairedDevicesKey, a.pairedDevices, devices[:min(len(devices), maxPairedDevices)])
}
