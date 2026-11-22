---
name: scenra
description: The Scenra file format (.scenra) of app designs and flow charts, and how a project of them is laid out. Use when asked to design, draw or change an app's screens, interfaces or flows in a .scenra file, or to build its designs in an NSGo app.
---

# Scenra files

Scenra files (`.scenra`) are plain text, and each holds one kind of thing: designs, drafts of how screens of an app look, or flow charts of workflows, such as how an order is paid and shipped, never both. An editor, such as the Scenra app, lists a folder's Scenra files and draws the one open, laying out the designs or flows of a file side by side. A project is a folder of them, each kind in its place, so the user finds everything from the windows and screens the app opens:

- The top of the open folder holds those windows and screens, a design file each, named after it, such as `sign-in.scenra` or `settings.scenra`.
- `views/` holds the views, the designs that fill a box of another design, such as a settings window's pages or a main window's content, each starting with `in=`, as Windows and outlets says, such as `views/settings-theme.scenra`. A design whose first line has `in=` goes there, and one without it at the top.
- `flows/` holds the flow charts.
- `components/` holds the pieces designs share.

Let a design show what happens on it, as the user sees it there: taps opening the screens it leads to, outlets showing a window's pages inside it, as Windows and outlets says, and notes on elements for what the drawing cannot show, such as when a block appears. Never draw a flow for one screen. A flow is a workflow of its own, a process that spans screens or runs without them, such as checkout or an agent working on an issue, in `flows/`, named in English for what it does, such as `flows/checkout.scenra`, never after a screen; the editor lists flows apart from designs, and a design's note may name the flow it starts. Draw one design or one flow in a file: give another screen, a screen's other state or another flow a file of its own, and put several designs or several flows in one file only when the user asks for them together. Keep every file directly in its place, never in folders of their own, such as `views/settings/theme.scenra`, which the editor does not show; as the app grows, group designs by their names instead: the first design's `name=` may start with one group and `/`, such as `design name="Orders/Order detail"`, and the editor lists the file under that group by the rest, so never repeat the group in it: write `Settings/Workflows`, not `Settings/Settings · Workflows`. Groups do not nest, so write one `/` at most. Flows and views take no groups: the editor lists each kind in a group of its own, under the components, by their whole names, so give views names that say their window, such as `Settings/Theme`. When the user only wants a flow chart, make only a flow file, still in `flows/`. Put the pieces several screens share, such as a tab bar, a navigation bar or a card, in components, which live in one folder, `components/`, at the top of the open folder, without folders of their own; group them by name as designs are, such as `component name="Forms/Text field"`, and the editor shows each group's components together. Put the pictures designs show in one folder, `assets/`, beside it.

## Working on files

1. Read the files and edit them, or make new ones in the folder, with your own file tools. Change only what the request needs, keep the IDs and files that exist and never invent screens or nodes the user did not ask for without saying so.
2. Say briefly what you changed. Keep labels short and put detail in notes. Write the interface's texts in the user's language.

When the editor tells you what the user sees and reports the problems of the files, as the Scenra app's MCP server does, the scenra-app skill says how to ask it; the problems it reports are those this skill names. Without an editor, a check tool your host gives, if any, reports the same problems, except how the designs lay out; run it after writing the files.

The editor saves files in their canonical form, which drops `#` comments, so do not keep notes in them.

When a design or flow is about code at hand, read the code first and draw what it actually does, naming the files behind a step in its note when that helps.

When asked to build designs in an NSGo app, read [references/nsgo.md](references/nsgo.md) for the components each one becomes.

## Format

A file's first line says what it holds: `design`, with `device=` phone (390 by 800 points, the default), tablet (820 by 1080) or desktop (1200 by 760), and optional `width=` and `height=` in points, 100 to 4000, which replace the device's size where the screen needs another, such as `design device=desktop width=1440 height=900` or a long page; `component`, a piece designs share, with `width=`, `width=fill`, `height=`, `device=` and `backdrop=` as Components says, or `flow`, with `direction=down` (the default) to lay it out top to bottom or `direction=right` left to right. Any of these lines may take `name="…"`, the name the editor shows, such as `design name="Sign in" device=phone`; the first line's names the file in place of its file name. Give one when a readable title in the user's language says more than the file name, which stays short and in English.

When the user asks for them together, a file may hold several designs, or several flows, never both: each starts with its own `design` or `flow` line, a blank line after the one before, and then each needs an ID right after the word, letters, digits, `_`, `-` or `.`, unique in the file, such as `design sign-up name="Sign up"`; the editor shows each under its name, or else its ID. A file holds one component. The editor packs a file's designs or flows in rows with the least margin, `gap=` points apart, 80 unless the file's first line sets another from 1 to 2000, such as `design sign-in gap=120`: widen it for large desktop designs or wide flows, and narrow it for small components or a few short flows.

```
# A comment runs to the end of the line; indentation is free.
design device=phone
box class="flex-1 gap-4 bg-zinc-50 p-6 pt-16"
  text "Welcome back" class="text-3xl font-bold text-zinc-900"
  text "Sign in to order ahead." class="text-zinc-500"
  box class="h-12 flex-row items-center gap-3 rounded-xl border border-zinc-200 bg-white px-4"
    icon mail class="size-4 text-zinc-400"
    text "you@example.com" class="text-zinc-400"
  end
  box class="h-12 items-center justify-center rounded-xl bg-amber-600 shadow-md"
    text "Sign in" class="font-semibold text-white"
  end
  image "Latte art seen from above" class="h-40 rounded-2xl"
  svg "<svg viewBox='0 0 120 40'><path d='M0 32 L30 20 L60 26 L90 8 L120 14' stroke='#d97706' stroke-width='3' fill='none'/></svg>" class="h-16 w-full"
end
```

```
design sign-in name="Sign in" gap=120
box class="flex-1 gap-4 p-6 pt-16"
  text "Sign in" class="text-3xl font-bold"
  text "No account? Sign up" class="text-amber-700" tap="open #sign-up"
end

design sign-up name="Sign up"
box class="flex-1 gap-4 p-6 pt-16"
  text "Create your account" class="text-3xl font-bold"
end
```

```
flow direction=right
node start "Tap Sign in" kind=start
node check "Check password" kind=decision note="Lock after 5 tries"
node home "Show home" kind=end status=done
start -> check
check -> home "valid"
check ..> start "invalid"
a -> b -> c
```

- A design is one element a line, and a `box` holds the lines up to its `end`.
- `node <id> "<label>"` is a step of a flow, with optional `note="…"`, `kind=` task (the default), start, end, decision, data or note, and `status=` todo, doing, done, blocked or skipped, when the chart tracks progress; skipped fades the step.
- `a -> b "label"` connects nodes, `a ..> b` dashed for optional or indirect links, and `a -> b -> c` chains them. An edge may name nodes no line declares; they take their ID as their label.
- Node IDs are letters, digits, `_`, `-` or `.`, unique in their flow; each flow of a file has its own nodes, and edges join nodes of their own flow.
- The file's name is its title: there is no title line.
- Quote a value with spaces or quotes in it; inside quotes, write `\"` for a quote, `\\` for a backslash and `\n` for a new line, such as `text "Say \"hi\""`.

Use `kind=start` and `kind=end` for entry and exit points and `kind=decision` for branches, with the answers as edge labels.

## Designs

A design is drawn with a few primitives, an element a line, each styled with Tailwind CSS 4 classes in `class="…"`:

- `box`: a rectangle laying out what it holds, a column unless its class has flex-row: with classes, any bar, panel, card, button, field, pill, circle or line
- `text`: its text, which wraps; size, weight and color it with classes
- `icon`: the Lucide icon its text names, as high as its font size: size it with size-* and color it with text-*
- `image`: a picture its text describes: the one `src="…"` names, a file of `assets/` such as `src="assets/latte.jpg"` or a URL, or else a placeholder; size it with classes; see Pictures
- `svg`: any drawing, such as a chart, a logo or an illustration: its text is an SVG document, quoted with single quotes inside, sized by its class or else its viewBox
- `use`: the component its text names, a file of `components/`, or else the design or view of that name, drawn as the component's element, its classes added; see Components
- `box` with `outlet="…"`: a place designs of other files fill; see Windows and outlets

Draw any interface with them, as a designer draws in Figma: buttons, fields, switches, tabs, cards, lists, charts and navigation are boxes, texts, icons and SVG shaped by classes, with exactly the look the app needs. Make designs look like a finished product, with a clear hierarchy, consistent spacing and realistic content, not wireframes.

Layout is flexbox: a box is a column unless flex-row, children stretch across it, flex-1 fills the rest, and absolute with top-*, left-*, right-* or bottom-* places a box over its parent, as a badge or a floating button. Classes cover sizes (w-*, h-*, size-*, w-full, w-1/2), spacing (p-*, gap-*, m-*), colors (bg-*, text-*, border-*, with /opacity such as bg-black/40, and any value as bg-[#0f172a]), the palette (zinc, slate, blue, amber, emerald, rose and the others, 50 to 950), corners (rounded-*, rounded-full), borders, shadows (shadow-sm to shadow-2xl), gradients (bg-linear-to-br from-indigo-500 to-pink-500), opacity, overflow-hidden, text (text-xs to text-6xl, font-medium, font-bold, leading-*, tracking-*, text-center, truncate, line-clamp-2, uppercase) and theme colors that follow light and dark mode (bg-background, text-foreground, bg-card, bg-primary, text-primary-foreground, bg-muted, text-muted-foreground, border-border).

The design is its size, the device's or its own, and clips what overflows; change `width=` or `height=` when the content needs room or the user asks for a screen size; give the main box flex-1 to fill it, and draw the status bar, navigation bars and tab bars yourself where the app has them. The design is the screen's frame, drawn on bg-background with an outline and the device's corners: shape it with `class="…"` on its line, such as `design device=desktop class="bg-zinc-50"` for another background or `class="rounded-none border-0"` for a frameless window, rather than with a box drawing a second background, border or corners inside it. Likewise, a component whose `backdrop=` gives what it sits on leaves that color off its own elements.

An icon takes the name of any Lucide icon (lucide.dev), by its current name or an older one such as home or alert-circle; common ones are house, search, bell, settings, user, users, circle-user, mail, message-circle, message-square, phone, send, inbox, heart, star, bookmark, share-2, link, paperclip, image, camera, video, mic, music, play, pause, calendar, clock, map-pin, navigation, compass, globe, shopping-cart, shopping-bag, store, credit-card, wallet, gift, tag, package, truck, file, file-text, folder, download, upload, cloud, plus, minus, x, check, pencil, trash, copy, funnel, sliders-horizontal, list, layout-grid, layout-dashboard, menu, ellipsis, ellipsis-vertical, chevron-right, chevron-left, chevron-down, chevron-up, arrow-right, arrow-left, arrow-up, arrow-down, external-link, refresh-cw, rotate-ccw, log-in, log-out, lock, key, shield, eye, eye-off, info, circle-alert, triangle-alert, circle-check, circle-x, circle-question-mark, chart-bar, chart-line, chart-pie, activity, trending-up, trending-down, zap, sparkles, flame, award, trophy, target, sun, moon, wifi, battery, smartphone, monitor, qr-code, scan, book, briefcase, building, coffee, utensils, car, plane, bike, dumbbell, headphones, graduation-cap. Draw a logo, a chart or an illustration with svg.

## Pictures

Keep every picture a design shows in `assets/` at the top of the open folder, as a PNG, JPEG, GIF or WebP file, and name it by its path there: `image "Latte art seen from above" src="assets/latte.jpg" class="h-40 rounded-2xl"`. When the user gives you a picture, or the code at hand has the app's own, such as its logo or its photos, copy it into `assets/`, in folders such as `assets/products/` as they grow, under a short English name, rather than pointing at it where it is: the editor shows only pictures in `assets/`, so the folder keeps working wherever it moves. Use a URL only for a picture that stays online. Without a picture, leave `src` out: the editor draws a placeholder its text describes, so write the text as what the picture shows. Never write a picture's path you have not made. The editor reports the pictures images cannot show.

Elements are named by their paths: an element's index among its siblings at each level from 0, joined by dots, so 1.0 is the first element the second top-level element holds of its design, which it names by its ID in a file of several.

## Components

A component is a piece of interface several designs or components share, drawn once in a file of `components/` at the top of the open folder, so changing it changes every screen using it. Its first line is `component`; the rest is elements, as in a design. The editor draws it bare, outlined, as large as its elements need, so a button shows as a button. Give it `width=` in points for a piece of a fixed width, or `width=fill` for one that fills the width where it is used, such as a list row, a sidebar item or a tab bar, which the editor shows `preview=` points wide, or else at its `device=`'s width, phone unless set: set `preview=` to the width it has in the app, such as `component width=fill preview=224` for a sidebar item, so its spacing and truncation show true. `width=`, `preview=` and `height=` size only the component's own drawing, never its uses, which their classes size. `backdrop="…"` gives the classes of what it sits on in the app, such as `backdrop="bg-zinc-100"` for an item of a grey sidebar, so its look and selected state show as they will; its elements draw over it. Separate layers with `>`, from the bottom, as `backdrop="bg-background > bg-foreground/5"` for a translucent sidebar over the window; one layer without `>` draws over `bg-background`, so translucent colors show right in light and dark mode. `use <name>` draws the component named by its file name in `components/` without `.scenra`: `use tab-bar` draws `components/tab-bar.scenra`. Any design draws as a component too, by the name taps give it, at the top or in `views/`, as its first design draws: `use order-list` draws `views/order-list.scenra` where no component has that name, and changes its elements by their names as a component's use does. Keep `components/` for the general pieces many screens share, which the editor shows together, and use a design or view itself where a screen shows another, such as a page inside a dashboard.

```
# components/tab-bar.scenra
component
box class="flex-row border-t border-zinc-200 bg-white px-2 pt-2 pb-7"
  box class="flex-1 items-center gap-1"
    icon house class="size-6 text-amber-600"
    text "Home" class="text-[10px] font-medium text-amber-600"
  end
end
```

```
# menu.scenra
design
box class="flex-1 bg-zinc-50"
  text "Menu" class="p-5 text-2xl font-bold"
  use tab-bar class="mt-auto"
end
```

- A use is one element: it draws as the component's element at its top, its classes added after that element's, so they win, as React passes `className` to a component's root; `use composer class="w-full max-w-2xl"` lays out as `box class="w-full max-w-2xl"` written there. Give a component one element at its top: one of several draws inside a box with the use's classes. A use takes no end unless it fills slots.
- Make a component of a piece the app's screens repeat, such as their tab bar, or when the user asks for one.
- Components may use other components, and fill their slots with them, never themselves, directly or through others, as `a` using `b` whose fill uses `a`.
- Write a component as a whole, the same on every screen, and leave what pressing it does to the designs: name the elements a use makes tappable and write no `tap=` in it, as Interactions shows.
- The editor lists the components there are and reports uses of missing ones, loops and changes that reach nothing.

### Changing a component where it is used

As in Figma's instances or Sketch's symbols, a use changes the values of its component's elements that have a `name=`, of letters, digits, `_` and `-`, so one component draws every row of a list or every variant of a card:

```
# components/menu-item.scenra
component
box name=row class="flex-row items-center gap-3 py-3"
  box name=badge class="size-10 items-center justify-center rounded-xl bg-amber-100"
    icon coffee name=icon class="size-5 text-amber-700"
  end
  text "Latte" name=title class="flex-1 font-medium"
  text "$4.20" name=price class="font-semibold"
  use tag name=tag
end
```

```
use menu-item
use menu-item title="Cold brew" price="$4.00" icon=zap icon.class="text-sky-700" badge.class="bg-sky-100" row.tap="open cold-brew"
use menu-item title="Matcha" tag/label="New"
use menu-item title="Mocha" tag.class="hidden"
```

- `<name>="…"` replaces the text of the elements of that name: a text's words, an icon's name, an image's description, an SVG document, or the component a `use` draws, swapping it for another. A box has no text.
- `<name>.class="…"` adds classes to theirs, which win over the ones they conflict with, so it recolors, resizes or, with `hidden`, hides them; `<name>.src="…"` replaces an image's picture and `<name>.tap="…"` an element's tap.
- `<name>.panel="…"` makes them a panel a tab shows, as `panel=` does, and `<name>.note="…"` notes them where the use is: the note is the using file's, as a note on the use itself is.
- `<name>.variant="…"` picks the variants of the component a `use` of that name draws, and `<name>.state="…"` puts the elements in states, as Variants says.
- `<a>/<b>="…"` changes the elements named b of the component the use named a draws, and so on down.
- Every element of a name changes, so give one name to several elements that show one value. Name the elements that vary between uses, and keep the component's own values realistic: they show where a use changes nothing.
- A name is letters, digits, `_` and `-`, and never a word elements take: name, class, tap, panel, comment, note, variant, state, outlet or src.

### Variants

A component's file declares its states with `variant <id>` lines after its elements, each changing it as a use would, with `class="…"` and its names: the editor draws every variant beside the component, so a switch's on and off, a button's kinds or a row's selected state show at once, as the props of the code to come. Uses pick them by ID with `variant=`, so a change to a variant reaches every use picking it.

```
# components/button.scenra
component
box name=button class="h-10 items-center justify-center rounded-lg bg-blue-600 px-4"
  text "Save" name=label class="font-medium text-white"
end
variant secondary button.class="bg-zinc-100" label.class="text-zinc-900"
variant disabled class="opacity-50"
```

```
use button variant=secondary label="Cancel"
use flow-step variant="doing late" title="Open the PR"
use settings/agent-item switch.variant=off
```

- `variant=` takes one or more variant IDs apart by spaces. The component draws first, then each variant in the order written, then what the use changes itself: the later wins, classes written later over earlier ones and a name's text set again over the one before.
- A variant may start from others of its component with `variant=`, such as `variant late variant=next icon.class="text-rose-600"`, never in a loop.
- `state=` puts the component's element at its top in a state, as pointing at or tapping it would: selected, hover, pressed, disabled or focus. Its `selected:` or `hover:` classes apply, and the `group-selected:` or `group-hover:` classes of what any `group` inside it holds; `state=selected@<name>` puts the elements of that name in it instead, for a component whose top is no group. Write the selected look once with `selected:` and `group-selected:` classes, and give the variant `state=selected` rather than the same classes again: `variant selected state=selected`, then `use tab variant=selected`. A use may write `state=` too; a box whose tap leads to what shows is selected as well.
- Pick a variant rather than writing out its changes on a use: the editor suggests where a use's changes are a variant's, and reports variants a component does not have, variants picking each other in a loop and states that draw nothing.

### Slots

A component leaves room for what each use puts in it with `slot`, as Vue's slots do: a box shaped by its classes, holding up to its `end` what it shows when a use fills nothing there. `slot <name>` names it; `slot` alone is the default slot. A use fills them with `fill <name>`, or `fill` for the default slot, each right after the use and holding any elements up to its `end`, and the use then takes an `end` after its fills:

```
# components/card.scenra
component
box class="gap-3 rounded-2xl bg-white p-4 shadow-sm"
  slot header class="flex-row items-center gap-2"
    text "Title" class="font-semibold"
  end
  slot class="gap-2"
  end
end
```

```
use card class="mx-5"
  fill header
    icon coffee class="size-5 text-amber-700"
    text "Your order" class="font-semibold"
  end
  fill
    use menu-item title="Latte"
    use menu-item title="Mocha"
  end
end
```

- Use a slot where uses differ by more than values, such as a card's body, a sheet's content or a bar's actions; change values by names instead.
- What a fill holds belongs to the file using the component: it is selected, commented and noted there, and its paths run through the use and the fill, so `0.1.0` is the first element of the second fill of the use at `0`.
- A fill inside a component may hold that component's own slot, passing on what its uses fill.
- A slot's name names it as `name=` does, and it may have a `name=` too: a variant or a use shapes it with `<name>.class="…"`, such as `variant stacked control.class="w-full"` or `use settings/item control.class="justify-end"`, its classes adding up as an element's do, the component's, then each variant's, then the use's. That is all a use changes of a slot: its text, tap and the rest are what the fill holds, which stays the using file's, and the editor reports `control="…"` or `control.tap="…"` on one.
- A fill takes only `comment=` and `note=`; a design has no slots. The editor reports fills of slots a component does not have and slots of one name written twice.

## Interactions

The editor plays a design as the app would run: pressing an element with `tap="…"`, or one inside it, does what the tap says, so the user can click through the screens.

- `tap="open <design>"` opens another design, named by its file name without `.scenra`, such as `tap="open menu"` or `tap="open order-detail"`, which opens the file's first design; add `#` and an ID for another design of a file, such as `tap="open auth#sign-up"`, or write `#` and the ID alone for one of the same file, such as `tap="open #sign-up"`, which the editor moves to.
- `tap="close"` closes the screen shown, returning to the design that opened it, as a back button, a dialog's Cancel or a sheet's close button.
- `tap="close window"` closes the window a design filling an outlet shows in, the outermost design of its `in=`, returning to the design that opened that, as the ✕ of a settings page closes the settings window; `tap="close <design>"`, such as `tap="close settings#general"`, closes that one of the designs it shows in. Each does nothing where no design opened the window.
- `tap="tab <name>"` shows the elements with `panel=<name>` in place of the panels beside them. The first panel among its siblings shows until a tab shows another, so draw the panels of one place side by side, as the content of a segmented control or of tabs.

A box whose tap leads to what shows, opening the design it is in or showing its panel there, is selected: style it with `selected:` classes, and what it holds with `group-selected:` classes and `group` on the box.

```
box class="flex-row gap-2"
  box class="group rounded-full px-4 py-2 selected:bg-zinc-900" tap="tab coffee"
    text "Coffee" class="text-zinc-700 group-selected:text-white"
  end
  box class="group rounded-full px-4 py-2 selected:bg-zinc-900" tap="tab tea"
    text "Tea" class="text-zinc-700 group-selected:text-white"
  end
end
box class="gap-2" panel=coffee
  text "Latte"
end
box class="gap-2" panel=tea
  text "Matcha"
end
```

Taps belong to the designs, as the panels they show do: a component gives its items names and the selected styles but no taps, and each use gives their taps with `<name>.tap="…"`, which play and mark what shows as if written there. Where items differ by more than their taps, leave a slot and write them in the fill.

```
# components/sidebar.scenra
component
box class="gap-1 p-3"
  box name=inbox class="group rounded-lg px-2 py-1.5 selected:bg-zinc-200"
    text "Inbox" class="text-zinc-600 group-selected:font-medium"
  end
  box name=archive class="group rounded-lg px-2 py-1.5 selected:bg-zinc-200"
    text "Archive" class="text-zinc-600 group-selected:font-medium"
  end
end
```

```
box class="flex-1 flex-row"
  use sidebar inbox.tap="tab inbox" archive.tap="tab archive"
  box class="flex-1" panel=inbox
    text "No new mail"
  end
  box class="flex-1" panel=archive
    text "Nothing archived"
  end
end
```

A tab bar's uses open the screens instead, such as `use tab-bar home.tap="open home" orders.tap="open orders"` on each, so every screen marks its own tab. Add taps where the user asks for a clickable prototype or where they make the flow between screens clear; open only designs that exist or that you make.

## Windows and outlets

Draw a window's frame, such as a sidebar beside a content area, once, and let the views of what it shows fill its content: mark the box they replace with `outlet=<name>`, whose own elements show until one does, put each view in `views/` and start it with `in="<file>#<outlet>"`, naming the host's file without `.scenra` or its folder, as taps do, and the outlet, or `in="<file>#<design>/<outlet>"` for an outlet of another design of a file of several; `#<design>/<outlet>` alone names one of the same file.

```
# main.scenra
design device=desktop name="Main window"
box class="flex-1 flex-row"
  box class="w-56 shrink-0"
    box class="group selected:bg-zinc-200" tap="open issues"
      text "Issues" class="group-selected:font-medium"
    end
  end
  box class="flex-1 rounded-xl bg-white" outlet=content
    text "New task"
  end
end
```

```
# views/issues.scenra
design in="main#content" name="Issues"
box class="flex-1"
  text "Issues" class="text-xl font-semibold"
end
```

- Taps and `in=` name a design by its file name alone, at the top or in `views/`, such as `tap="open issues"`, never `views/issues`, so moving a design between them breaks nothing; never give a view the name of a design at the top.
- A design filling an outlet draws alone at the size the outlet lays out at, framed as the outlet is, by its background, border and corners, rather than as a device, so it needs no `device=`, `width=` or `height=`, those it writes winning, and draws no frame of its own: shape the outlet's box where the window shows it.
- It plays inside its host: opening it, from the host or a design its outlet shows, swaps only the outlet's content, so the rest of the window stays and keeps working, and what opens it is selected. `tap="close"` returns the outlet to what it showed before, or to its own elements; `tap="close window"` closes the whole window, as Interactions says.
- A host may fill an outlet of its own, as the categories of a settings window fill its content, which fills the main window.
- The first design's `in=` stands for the others of its file that write none.
- The editor reports outlets `in=` names that are not there, designs filling each other in a loop, two outlets of one name in a design, and designs with `in=` at the top or without it in `views/`.

## Comments and notes

The user comments on elements to ask for changes: an element's `comment="…"` is the user's, not part of the design. Do what a comment asks, or discuss it when it is unclear, then remove the attribute and say what you did. Keep comments on the elements you rewrite until they are resolved, and never add one yourself.

An element's `note="…"` says what the design cannot show, such as what it does, where its data comes from or when it changes. Notes stay: read them as part of the design, keep them true as you change it, and add one when the user asks or when a behavior matters and the design cannot show it.
