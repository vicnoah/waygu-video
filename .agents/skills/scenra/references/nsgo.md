# Building designs in NSGo

A Scenra design is a draft drawn from a few primitives, so a button, a field and a tab bar are all boxes there. Building it in an NSGo app means telling which boxes stand for controls, which for what the platform draws, and which stay views. This maps Scenra to NSGo; read the `nsgo` skill, when you have it, and the NSGo source of the app's version for each API, as NSGo changes quickly.

Before writing code, read the design's file with its notes, the components it uses, the views filling its outlets and the flows it starts; in the Scenra app, ask its MCP server for the problems and, when a value is unclear, the sizes, as the scenra-app skill says. Comments are requests not yet resolved, not part of the design.

## Primitives

Scenra draws designs with NSGo's own elements and Tailwind classes, so what it draws, an app draws the same:

| Scenra | NSGo |
| --- | --- |
| `box` | `core.View{Class: …, Children: …}` |
| `text` | `core.Text{Content: …, Class: …}`, its words from the app's `locales/` |
| `icon` | `core.Icon{SVG: lucide.…, Class: …}` from `icons/lucide`, the name in Pascal case: `share-2` is `lucide.Share2`, and an older name its current one's, such as `home`, `lucide.House` |
| `image` | `core.Image{Source: core.Src("asset:…"), ResizeMode: core.Cover}`, the picture copied from `assets/` into the app's `resources/`; one without `src=` is a picture the app loads |
| `svg` | `core.SVGView` of a logo or illustration kept in `resources/`; a chart is `base/plot` drawing the app's data |

- Copy `class="…"` into `Class` as written: layout, spacing, `selected:`, `hover:`, `active:`, `group` and `group-*:` work alike.
- Fixed palette colors, such as `bg-amber-600`, are the draft's: give the app's accent and surfaces to the theme and use its roles, such as `bg-primary`, `text-muted-foreground` or `ui.ColorsOf`, so dark mode and themes follow.
- A design's content, the names, prices and messages, is sample data: the app's data shows there, and only its labels go to `locales/`, in every language the app has.

## Controls

A box drawn as a control is `ui`'s control, never a view copying its look, which would lose focus, keyboard, accessibility and native behavior. Match the look with the control's `Variant`, `Size`, `Style`, `Color`, `Tint` or the theme; only a look no `ui` control takes is `base`'s unstyled control with classes.

| Drawn | NSGo |
| --- | --- |
| A box of a label, with a tap or meant as an action | `ui.Button`: filled in the accent the default, grey `ButtonSecondary`, bordered `ButtonOutline`, bare `ButtonGhost`, full width with `Style: core.Style{Width: core.Pct(100)}` |
| An icon alone in a box | `ui.IconButton` with a `Label` |
| A bordered box with grey text or an icon, a field | `ui.Input` with a `Placeholder` and a `Label`, `Multiline` or `Masked`; `ui.FormField` with the label and message drawn around it; `ui.NumberInput`, `ui.OtpInput`, `ui.TagInput` |
| A pill with a knob, a ticked square, rings of a choice | `ui.Switch`, `ui.Checkbox`, `ui.Radio` in a `ui.RadioGroup` |
| A box with a value and a chevron down | `ui.Select[T]`, or `ui.Combobox` when typing filters it |
| A track with a thumb, stars | `ui.Slider`, `ui.Rating` |
| A date, a time, a color | `ui.DatePicker`, `ui.Calendar`, `ui.TimeField`, `ui.ColorPicker` |
| A segmented control or tabs whose taps show `panel=`s | `ui.Tabs[T]`, its `Value` the signal choosing the panel that shows |
| A capsule label, a round picture or initials | `ui.Badge`, `ui.Avatar`, `ui.AvatarGroup` |
| A bar, a ring, a spinner, grey blocks while loading | `ui.Progress`, `ui.ProgressRing`, `ui.Spinner`, `ui.Skeleton` |
| Rows of a title, a description and a control | `ui.Settings` for a settings window, `ui.FormSection` with `ui.FormRow` for a form |
| Repeated rows or cards | `core.Each` over the data; `ui.List` or `core.List` for long data, which builds only the rows in view |
| A tree of rows, a table | `ui.Tree`; `base.Table`, or `base/datatable` for large ones |
| A row of icon buttons | `ui.Toolbar` |
| A menu, a popover or a tooltip drawn open | `ui.Menu` with `ui.MenuItem`, `ui.ContextMenu`, `ui.Popover`, `ui.HoverCard`, `ui.Tooltip` |
| A dialog over a dimmed screen, a sheet from the bottom, a toast | `ui.Dialog` or `ui.AlertDialog`, `ui.Sheet`, `ui.Toaster` |
| A conversation, Markdown, code, a flow chart | `ui/chat`, `base/textview`, `base/editor`, `ui/flow` |

Cards, panels and other boxes of no behavior stay `core.View`s with their classes.

## What the platform draws

- A phone's status bar is the system's: draw none, and keep content clear of it with `PaddingEnv: core.EnvEdges{Top: core.EnvSafeArea}`.
- A phone's navigation bar, a title with a back button and actions, is `ui.NavPage` with a `ui.NavHeader` in a `base.NewNav` stack, which iOS and Android draw as their own.
- A phone's tab bar is `ui.TabShell`, each tab a `ui.ShellTab` holding its section's stack, given a `SystemIcon` so iOS and Android draw their own bar.
- A desktop window's title bar is the window's. A sidebar beside the content is `ui.SidebarShell` with a `ui.Sidebar` of sections and items, its `Selected` marking the item shown.
- Everything inside them is the app's.

## Screens, views and taps

- A design at the top of the folder is a screen or window the app opens: the root of `nsgo.Run`, a route of a `base.Nav` stack, or a window `desktop.OpenWindow` opens. The background its `class=` gives is that screen's or window's; its outline and corners are the editor's frame, which the platform draws.
- A box with `outlet=` is where the host shows the views of `views/` that name it: a `core.Dynamic` or `core.Show` reading a signal of the view shown, or a shell's `Content`. Each view is a component of its own.
- `tap="open …"` is `nav.Push` of the route, setting the signal of the outlet showing it, opening the window, or a dialog's `Open`; `tap="close"` is `nav.Pop(1)`, the outlet's previous view or the dialog closing, and `tap="close window"` the window's `Close`.
- `tap="tab …"` and `panel=` are a signal the tabs or the sidebar set and the panels show by.
- A box's `selected:` look is `View.Selected`, a `func() bool` reading that signal, or the control's own selection, such as `ui.Tabs`' `Value` or `ui.Sidebar`'s `Selected`, which brings its look.
- Notes say what to build that the drawing cannot show: where data comes from, when a block appears, what an action does. The flows a design starts say the logic across screens: state, loading with `core.NewResource`, and the order of steps.

## Components

A Scenra component the app draws itself, not one standing for a `ui` control or the platform's chrome, is a Go type with `Render(*core.Scope) core.Node`, its top element the view `Render` returns:

- The elements a use names are its fields: a text a `string` or a `func() string` from a signal, an icon a `func() *core.SVG`, a picture an `core.ImageSource`, and a `<name>.tap` an `OnPress`-like `func()`.
- Its variants are a `Variant` type of its own whose zero value is the component drawn without one, as `ui.ButtonVariant` is, and its states fields such as `Selected func() bool` and `Disabled func() bool` the view reads, never a variant per state.
- Its default slot is `Children []core.Node`, and a named slot a `core.Node` or `[]core.Node` field of its name, as `ui.SidebarShell` takes `Header`, `Actions` and `Content`.
- A use's classes are a `Style core.Style` merged over the top view's, as `ui`'s components take.
- Uses of one component down a list are `core.Each` over data rather than a line each.
