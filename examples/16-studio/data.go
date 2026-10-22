package main

import (
	"github.com/GofMan5/lux/render"
	"github.com/GofMan5/lux/scene3d"
	"github.com/GofMan5/lux/ui"
)

// theme is one switchable look. The palette drives every widget through
// ui.Palette.Theme; ok/warn/bad color the status glyphs that are data, not
// selection, so they stay readable on all three palettes.
type theme struct {
	name          string
	palette       ui.Palette
	ok, warn, bad render.Color
}

var themes = []theme{
	{name: "Aurora", palette: ui.ReferencePalette(),
		ok: render.RGBColor(52, 211, 153), warn: render.RGBColor(251, 191, 36), bad: render.RGBColor(248, 113, 113)},
	{name: "Ember", palette: ui.Palette{
		Background: render.RGBColor(20, 12, 10),
		Ink:        render.RGBColor(245, 232, 222),
		Muted:      render.RGBColor(146, 116, 104),
		Violet:     render.RGBColor(234, 88, 12),
		Cyan:       render.RGBColor(250, 204, 21),
	}, ok: render.RGBColor(163, 230, 53), warn: render.RGBColor(251, 146, 60), bad: render.RGBColor(239, 68, 68)},
	{name: "Matrix", palette: ui.Palette{
		Background: render.RGBColor(4, 13, 9),
		Ink:        render.RGBColor(214, 255, 224),
		Muted:      render.RGBColor(84, 132, 98),
		Violet:     render.RGBColor(22, 163, 74),
		Cyan:       render.RGBColor(134, 239, 172),
	}, ok: render.RGBColor(74, 222, 128), warn: render.RGBColor(250, 204, 21), bad: render.RGBColor(251, 113, 133)},
}

var themeNames = func() []string {
	names := make([]string, len(themes))
	for i, t := range themes {
		names[i] = t.name
	}
	return names
}()

var pages = []string{"Dashboard", "Services", "Files", "Motion", "Settings"}

const (
	pageDashboard = iota
	pageServices
	pageFiles
	pageMotion
	pageSettings
)

// service is one row of the Services page. The table shows the first four
// fields; the detail pane shows the rest, with url as an OSC 8 hyperlink.
type service struct {
	name, region, status string
	latency              int
	url, about           string
}

var seedServices = []service{
	{"api-gateway", "eu-west", "healthy", 38, "https://github.com/GofMan5/lux", "Edge router: TLS, auth, rate limits. Every request enters here."},
	{"auth", "eu-west", "healthy", 22, "https://github.com/GofMan5/lux/tree/main/ui", "Sessions and tokens. Stateless, scales on CPU."},
	{"billing", "us-east", "degraded", 214, "https://github.com/GofMan5/lux/issues", "Invoices and payment webhooks. Upstream provider is slow today."},
	{"catalog", "us-east", "healthy", 41, "https://github.com/GofMan5/lux/tree/main/render", "Product catalog reads, heavily cached."},
	{"checkout", "eu-west", "healthy", 67, "https://github.com/GofMan5/lux/tree/main/layout", "Cart to order pipeline with idempotent retries."},
	{"events", "ap-south", "healthy", 12, "https://github.com/GofMan5/lux/tree/main/event", "Fan-out bus for domain events."},
	{"inventory", "us-west", "down", 0, "https://github.com/GofMan5/lux/tree/main/motion", "Stock levels. Failed its last health check; restart it."},
	{"mailer", "eu-north", "healthy", 95, "https://github.com/GofMan5/lux/tree/main/scene3d", "Transactional mail with per-tenant throttling."},
	{"media", "us-west", "healthy", 130, "https://github.com/GofMan5/lux/tree/main/examples", "Image resize and CDN origin."},
	{"notifier", "ap-south", "degraded", 180, "https://github.com/GofMan5/lux/tree/main/capability", "Push and SMS. Queue depth above target."},
	{"search", "us-east", "healthy", 54, "https://github.com/GofMan5/lux/tree/main/docs", "Full-text and facet search over the catalog."},
	{"telemetry", "eu-north", "healthy", 9, "https://github.com/GofMan5/lux/tree/main/benchmarks", "Metrics ingest; feeds the dashboard you just saw."},
}

var serviceColumns = []ui.TableColumn{
	{Title: "SERVICE", Weight: 2},
	{Title: "REGION", Width: 9},
	{Title: "STATUS", Width: 10},
	{Title: "P95 ms", Width: 7},
}

// node is one entry of the Files page tree; leaves carry file content.
type node struct {
	id, label string
	children  []node
}

var projectTree = []node{
	{id: "cmd", label: "cmd/", children: []node{
		{id: "cmd/studio/main.go", label: "studio/main.go"},
	}},
	{id: "internal", label: "internal/", children: []node{
		{id: "internal/api", label: "api/", children: []node{
			{id: "internal/api/server.go", label: "server.go"},
			{id: "internal/api/routes.go", label: "routes.go"},
		}},
		{id: "internal/store/store.go", label: "store/store.go"},
	}},
	{id: "README.md", label: "README.md"},
	{id: "config.yaml", label: "config.yaml"},
}

var seedFiles = map[string]string{
	"cmd/studio/main.go":      "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello from lux studio\")\n}\n",
	"internal/api/server.go":  "package api\n\n// Server owns the listener and the router.\ntype Server struct {\n\tAddr string\n}\n",
	"internal/api/routes.go":  "package api\n\nvar routes = []string{\n\t\"/healthz\",\n\t\"/v1/services\",\n\t\"/v1/deploys\",\n}\n",
	"internal/store/store.go": "package store\n\n// Store is the in-memory service registry.\ntype Store struct {\n\tservices map[string]int\n}\n",
	"README.md":               "# LUX Studio\n\nThis editor is a ui.TextArea: select with Shift+arrows or the mouse,\nundo with Ctrl+Z, redo with Ctrl+Y, copy the selection with Ctrl+C\n(OSC 52 to the system clipboard) and save with Ctrl+S.\n\nWide text wraps: 漢字とカナ, emoji 🚀✨, combining é.\n",
	"config.yaml":             "region: eu-west\nreplicas: 3\nrollout:\n  strategy: canary\n  steps: [10, 50, 100]\n",
}

func flattenTree(nodes []node, expanded func(string) bool, depth int, rows []ui.TreeRow) []ui.TreeRow {
	for _, n := range nodes {
		rows = append(rows, ui.TreeRow{ID: n.id, Label: n.label, Depth: depth, HasChildren: len(n.children) > 0})
		if len(n.children) > 0 && expanded(n.id) {
			rows = flattenTree(n.children, expanded, depth+1, rows)
		}
	}
	return rows
}

var stages = []string{"build", "test", "package", "rollout", "verify"}

// stageSteps is how many job ticks each pipeline stage takes.
const stageSteps = 8

var easings = []string{"OutCubic", "InOutCubic", "OutBack", "Linear"}
var shapes = []string{"Cube", "Gem"}
var sampleRates = []string{"250 ms", "500 ms", "1 s"}

// gem is a hand-built octahedron: the Mesh API is plain vertices plus
// counter-clockwise triangles, so any shape is a few lines of data.
func gem() scene3d.Mesh {
	vertices := []scene3d.Vec3{{X: 1.1}, {X: -1.1}, {Y: 1.4}, {Y: -1.4}, {Z: 1.1}, {Z: -1.1}}
	colors := []scene3d.Color{
		scene3d.RGB(139, 92, 246), scene3d.RGB(34, 211, 238), scene3d.RGB(244, 114, 182), scene3d.RGB(250, 204, 21),
		scene3d.RGB(52, 211, 153), scene3d.RGB(96, 165, 250), scene3d.RGB(251, 146, 60), scene3d.RGB(167, 139, 250),
	}
	var triangles []scene3d.Triangle
	for i, sx := range []int{0, 1} {
		for j, sy := range []int{2, 3} {
			for k, sz := range []int{4, 5} {
				a, b, c := sx, sy, sz
				// Each negative axis mirrors the face, so an odd count of
				// them flips the winding back to counter-clockwise.
				if (i+j+k)%2 == 1 {
					b, c = c, b
				}
				triangles = append(triangles, scene3d.Triangle{A: a, B: b, C: c, Color: colors[i*4+j*2+k]})
			}
		}
	}
	return scene3d.Mesh{Vertices: vertices, Triangles: triangles}
}
