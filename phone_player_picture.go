package main

import (
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/locales"
)

type playerPictureMode string

const (
	pictureOriginal playerPictureMode = ""
	pictureFill     playerPictureMode = "crop"
	pictureStretch  playerPictureMode = "stretch"
	pictureWide     playerPictureMode = "16-9"
	pictureClassic  playerPictureMode = "4-3"
	pictureCinema   playerPictureMode = "2-35-1"
)

func (mode playerPictureMode) fit() core.ResizeMode {
	switch mode {
	case pictureFill:
		return core.Cover
	case pictureStretch, pictureWide, pictureClassic, pictureCinema:
		return core.Stretch
	default:
		return core.Contain
	}
}

func (mode playerPictureMode) ratio() core.Points {
	switch mode {
	case pictureWide:
		return 16.0 / 9
	case pictureClassic:
		return 4.0 / 3
	case pictureCinema:
		return 2.35
	default:
		return 0
	}
}

func (mode playerPictureMode) viewport(size core.Size) core.Style {
	ratio := mode.ratio()
	if ratio == 0 || size.Width <= 0 || size.Height <= 0 {
		return core.Style{}
	}
	x := max(0, (size.Width-size.Height*ratio)/2)
	y := max(0, (size.Height-size.Width/ratio)/2)
	return core.Style{Left: core.Pt(x), Right: core.Pt(x), Top: core.Pt(y), Bottom: core.Pt(y)}
}

func (p phonePlayerControls) picturePanel(cx *core.Scope, open *core.Signal[bool]) core.Node {
	label := locales.PhonePlayerPictureRatio(cx)
	choices := []struct {
		mode playerPictureMode
		id   string
		name string
	}{
		{pictureOriginal, "original", locales.SettingsPictureOriginal(cx)},
		{pictureFill, "crop", locales.SettingsPictureCrop(cx)},
		{pictureStretch, "stretch", locales.SettingsPictureStretch(cx)},
		{pictureWide, string(pictureWide), "16:9"},
		{pictureClassic, string(pictureClassic), "4:3"},
		{pictureCinema, string(pictureCinema), "2.35:1"},
	}
	rows := make([]core.Node, 0, len(choices))
	for _, choice := range choices {
		item := ui.Radio{Text: choice.name, TestID: "phone-player-ratio-" + choice.id, Checked: func() bool {
			return p.pictureMode.Get() == choice.mode
		}, OnChange: func(bool) {
			p.pictureMode.Set(choice.mode)
			open.Set(false)
		}}
		rows = append(rows, playerRadio(cx, item, "", false))
	}
	return playerSidePanel(cx, "phone-player-ratio-panel", "phone-player-ratio-close", label, open.Get,
		func() { open.Set(false) }, panelHeading(label), nil, playerRadioList(cx, label, rows))
}
