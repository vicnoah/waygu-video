package reference

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/nsgo-dev/nsgo" // Register the platform test host.
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"
	"github.com/oneo-me/nsgo-scenra/flowfile"
	"github.com/oneo-me/nsgo-scenra/scenraview"
)

func TestMain(m *testing.M) { nsgotest.Main(m) }

func TestDesignReferences(t *testing.T) {
	if nsgotest.Target() == "" {
		t.Skip("requires a screenshot target")
	}
	project := designProject(t)
	name := os.Getenv("WAYGU_REVIEW_DESIGN")
	if name == "" {
		name = "welcome.scenra"
	}
	names := []string{name}
	if name == "all" {
		names = nil
		for _, pattern := range []string{"*.scenra", "views/*.scenra"} {
			matches, err := fs.Glob(os.DirFS(project), pattern)
			if err != nil {
				t.Fatal(err)
			}
			names = append(names, matches...)
		}
	}
	panel := os.Getenv("WAYGU_REVIEW_PANEL")
	if name == "all" && panel != "" {
		t.Fatal("select a single Scenra file when capturing a panel")
	}
	for _, file := range names {
		if !filepath.IsLocal(file) || filepath.Ext(file) != ".scenra" {
			t.Fatalf("expected a .scenra file inside design/, got %q", file)
		}
		t.Run(strings.TrimSuffix(file, ".scenra"), func(t *testing.T) { captureDesign(t, project, file, panel) })
	}
}

func captureDesign(t *testing.T, project, name, panel string) {
	t.Helper()
	design := readDesign(t, project, name)
	for i, variant := range design.Screens {
		t.Run(variant.ID, func(t *testing.T) {
			single := design
			if panel != "" {
				elements, found := selectPanel(variant.UI, panel)
				if !found {
					t.Fatalf("panel %q does not exist in %s", panel, name)
				}
				variant.UI = elements
			}
			single.Screens = []flowfile.Screen{variant}
			width, height := design.Size(i)
			if hostName, end, hosted := strings.Cut(variant.In, "#"); hosted && hostName != "" {
				host := readDesign(t, project, hostName+".scenra")
				hostID, _, _ := strings.Cut(end, "/")
				if index, found := host.Screen(hostID); found {
					width, height = host.Size(index)
				}
			}
			// On the GTK test host the window height includes the 37 pt header.
			windowHeight := height
			if nsgotest.Target() == "linux" {
				windowHeight += 37
			}
			app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
				if variant.ID == "dark" {
					cx.SetSubtreeAppearance(core.AppearanceDark)
				}
				return scenraview.Still{Source: single.Format(), Project: project}
			}, nsgotest.Offscreen(), nsgotest.Size(core.Points(width), core.Points(windowHeight)),
				nsgotest.Environment(core.Environment{Locale: "zh-Hans"}))
			app.WaitFor("id:frame-0", 5*time.Second)
			if name == "welcome.scenra" {
				app.WaitFor("text:Waygu", 5*time.Second)
				app.WaitFor("text:稍后设置", time.Second)
			}
			if name == "tablet-home.scenra" {
				app.WaitFor("text:观看记录", 5*time.Second)
			}
			app.Advance(500 * time.Millisecond)
			stem := strings.TrimSuffix(filepath.ToSlash(name), ".scenra")
			if variant.ID != "" {
				stem += "-" + variant.ID
			}
			if panel != "" {
				stem += "-panel-" + panel
			}
			app.Screenshot(stem)
		})
	}
}

// Retain the requested panel among its sibling tabs. Still selects the
// first panel in each group, so the matching tab inherits its selected style.
func selectPanel(nodes []flowfile.Element, name string) ([]flowfile.Element, bool) {
	foundHere := false
	for _, node := range nodes {
		foundHere = foundHere || node.Attr("panel") == name
	}
	found := foundHere
	var out []flowfile.Element
	for _, node := range nodes {
		if panel := node.Attr("panel"); foundHere && panel != "" && panel != name {
			continue
		}
		if len(node.Children) != 0 {
			children, matched := selectPanel(node.Children, name)
			node.Children = children
			found = found || matched
		}
		out = append(out, node)
	}
	return out, found
}

func designProject(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		project := filepath.Join(dir, "design")
		if info, err := os.Stat(filepath.Join(project, "welcome.scenra")); err == nil && info.Mode().IsRegular() {
			return project
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find the project's design folder above the native test host")
		}
		dir = parent
	}
}
func readDesign(t *testing.T, project, name string) flowfile.File {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(project, name))
	if err != nil {
		t.Fatal(err)
	}
	design, err := flowfile.Parse(string(content))
	if err != nil {
		t.Fatal(err)
	}
	if design.Kind != flowfile.Design {
		t.Fatalf("%s is not a screen design", name)
	}
	return design
}
