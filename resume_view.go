package main

import (
	"strconv"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/icons/lucide"
	"github.com/nsgo-dev/nsgo/ui"

	"waygu-video/internal/media"
	"waygu-video/locales"
)

func (a *application) resumeShelfView(cx *core.Scope, items []catalogItem) core.Node {
	width, height := core.Pt(240), core.Pt(135)
	if cx.Env().Idiom() == core.IdiomTV {
		width, height = core.Pt(320), core.Pt(180)
	}
	cards := make([]core.Node, 0, len(items))
	for _, entry := range items {
		item := entry.item
		id := "resume-" + item.ID
		if len(a.sources.GetUntracked()) > 1 {
			id = "resume-" + entry.source.ID + "-" + item.ID
		}
		var cover core.Node = core.Icon{SVG: lucide.Film(), Class: "size-9 text-primary"}
		art := item.Backdrop
		if art == "" {
			art = item.Poster
		}
		if art != "" {
			cover = core.Image{Class: "absolute inset-0", Source: core.Src(art), ResizeMode: core.Cover, ElementsHidden: true}
		}
		mediaBox := core.View{Class: "relative items-center justify-center overflow-hidden rounded-xl bg-card",
			Style: core.Style{Height: height}, Children: []core.Node{cover}}
		description := itemMetadata(cx, item)
		if item.Kind == media.Episode && item.SeriesTitle != "" {
			description = item.SeriesTitle
			if item.Episode > 0 {
				description += " · " + locales.EpisodeLabel(cx, strconv.Itoa(item.Episode))
			}
		}
		if len(a.sources.GetUntracked()) > 1 {
			description += " · " + entry.source.Name
		}
		if item.Duration > 0 {
			percent := float32(100 * float64(item.Position) / float64(item.Duration))
			mediaBox.Children = append(mediaBox.Children, core.View{Class: "absolute inset-x-0 bottom-0 px-2 pb-2",
				Children: []core.Node{ui.Progress{Value: func() float32 { return percent },
					Label: locales.ResumePercent(cx, strconv.Itoa(int(percent))), TestID: id + "-progress"}}})
			description += " · " + locales.ResumeRemaining(cx, playbackClock(item.Duration-item.Position))
		}
		cards = append(cards, base.Button{Label: locales.ResumeCardLabel(cx, item.Title), TestID: id,
			Style: core.Style{Width: width, FlexShrink: 0, CornerRadius: 12, FocusVisible: ui.FocusRing(cx, 3)},
			OnPress: func(core.PressEvent) {
				a.push(route{screen: screenPlayer, source: entry.source, item: item, resume: true, resumeLocal: entry.pending})
			},
			Children: []core.Node{core.View{Class: "w-full gap-1", Children: []core.Node{mediaBox,
				core.Text{Content: item.Title, Class: "line-clamp-1 text-sm font-semibold"},
				core.Text{Content: description, Class: "line-clamp-2 text-sm text-muted-foreground"},
			}}}})
	}
	return core.Scroll{Horizontal: true, Style: core.Style{FlexShrink: 0}, Children: []core.Node{
		core.View{Class: "flex-row gap-4 pb-2", Children: cards},
	}}
}
