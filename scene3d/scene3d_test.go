package scene3d

import (
	"math"
	"testing"

	"github.com/lux-tui/lux/render"
)

func TestNearTriangleWinsRegardlessOfDrawOrder(t *testing.T) {
	near := triangleMesh(0, RGB(255, 0, 0))
	far := triangleMesh(-1, RGB(0, 0, 255))
	first := renderScene(t, []Object{{Mesh: near}, {Mesh: far}}, 24, 12)
	second := renderScene(t, []Object{{Mesh: far}, {Mesh: near}}, 24, 12)

	if !first.Equal(second) {
		t.Fatal("z-buffer result depends on object order")
	}
	cell := first.Cell(12, 6)
	red, green, blue, ok := cell.Style.Foreground.RGB()
	if !ok || red != 255 || green != 0 || blue != 0 {
		t.Fatalf("center foreground = (%d,%d,%d,%v), want near red", red, green, blue, ok)
	}
}

func TestNearPlaneClippingAndMalformedGeometry(t *testing.T) {
	mesh := Mesh{
		Vertices: []Vec3{{-.8, -.6, -2}, {.8, -.6, -2}, {0, .8, .1}},
		Triangles: []Triangle{
			{A: 0, B: 1, C: 2, Color: RGB(80, 220, 140)},
			{A: 0, B: 99, C: 2, Color: RGB(255, 0, 0)},
		},
	}
	buffer := render.NewBuffer(30, 12)
	renderer := NewRenderer()
	renderer.Render(render.NewCanvas(buffer), Scene{
		Objects: []Object{{Mesh: mesh}},
		Camera:  Camera{Target: Vec3{Z: -1}, Up: Vec3{Y: 1}, FOVY: Degrees(60), Near: .5, Far: 10},
		Light:   Light{Direction: Vec3{Z: 1}, Ambient: 1},
	})

	visible := 0
	for index := range buffer.Len() {
		if buffer.CellAt(index).Grapheme != " " {
			visible++
		}
	}
	if visible == 0 {
		t.Fatal("triangle crossing the near plane rendered no cells")
	}
}

func TestResizeAndTinyViewsReuseBuffers(t *testing.T) {
	renderer := NewRenderer()
	scene := Scene{Objects: []Object{{Mesh: Cube(1)}}}
	for _, size := range [][2]int{{0, 0}, {1, 1}, {80, 24}, {2, 1}, {160, 48}} {
		buffer := render.NewBuffer(size[0], size[1])
		renderer.Render(render.NewCanvas(buffer), scene)
		if size[0] > 0 && size[1] > 0 {
			want := size[0] * size[1] * 2
			if len(renderer.depth) != want || len(renderer.pixels) != want {
				t.Fatalf("buffers at %dx%d = %d/%d, want %d", size[0], size[1], len(renderer.depth), len(renderer.pixels), want)
			}
		}
	}
}

func TestNonFiniteCameraFallsBack(t *testing.T) {
	canvas := render.NewCanvas(render.NewBuffer(40, 16))
	renderer := NewRenderer()
	scene := Scene{
		Objects: []Object{{Mesh: Cube(1)}},
		Camera:  Camera{Position: V3(math.NaN(), 0, 3), FOVY: math.NaN()},
	}
	renderer.Render(canvas, scene)
	hit := false
	for index := 0; index < canvas.Buffer().Len(); index++ {
		if canvas.Buffer().CellAt(index).Grapheme != " " {
			hit = true
			break
		}
	}
	if !hit {
		t.Fatal("non-finite camera did not fall back to a visible default")
	}
}

func TestCubeTransformsAndANSIOutput(t *testing.T) {
	cube := Cube(2)
	if len(cube.Vertices) != 8 || len(cube.Triangles) != 12 {
		t.Fatalf("Cube geometry = %d vertices, %d triangles", len(cube.Vertices), len(cube.Triangles))
	}
	point := Translate(1, 2, 3).Mul(RotateY(Degrees(90))).TransformPoint(Vec3{X: 1})
	if !close(point.X, 1) || !close(point.Y, 2) || !close(point.Z, 2) {
		t.Fatalf("transformed point = %#v, want (1,2,2)", point)
	}

	buffer := render.NewBuffer(20, 10)
	renderer := NewRenderer()
	renderer.ColorMode = ANSI16
	renderer.Render(render.NewCanvas(buffer), Scene{Objects: []Object{{Mesh: cube, Transform: RotateY(.4)}}})
	for index := range buffer.Len() {
		cell := buffer.CellAt(index)
		if cell.Grapheme != " " && cell.Style.Foreground.Mode != render.ColorANSI {
			t.Fatalf("visible cell uses color mode %d, want ANSI", cell.Style.Foreground.Mode)
		}
	}
}

func TestMonochromeRamp(t *testing.T) {
	buffer := render.NewBuffer(20, 10)
	renderer := NewRenderer()
	renderer.ColorMode = Monochrome
	renderer.Ramp = " x"
	renderer.Render(render.NewCanvas(buffer), Scene{
		Objects: []Object{{Mesh: triangleMesh(0, RGB(255, 255, 255))}},
		Camera:  Camera{Position: Vec3{Z: 3}, Up: Vec3{Y: 1}, FOVY: Degrees(60), Near: .1, Far: 20},
		Light:   Light{Direction: Vec3{Z: 1}, Ambient: 1},
	})
	visible := 0
	for index := range buffer.Len() {
		cell := buffer.CellAt(index)
		if cell.Grapheme == "x" {
			visible++
		}
		if cell.Style.Foreground.Mode != render.ColorRGB || cell.Style.Background.Mode != render.ColorRGB {
			t.Fatalf("cell %d style is not RGB: %#v", index, cell.Style)
		}
	}
	if visible == 0 {
		t.Fatal("monochrome ramp rendered no lit cells")
	}
}

func BenchmarkRenderer(b *testing.B) {
	for _, size := range [][2]int{{80, 24}, {160, 48}} {
		b.Run(sizeName(size[0], size[1]), func(b *testing.B) {
			buffer := render.NewBuffer(size[0], size[1])
			canvas := render.NewCanvas(buffer)
			renderer := NewRenderer()
			scene := Scene{Objects: []Object{{Mesh: Cube(2)}}}
			renderer.Render(canvas, scene)
			b.ReportAllocs()
			b.SetBytes(int64(size[0] * size[1] * 2))
			b.ResetTimer()
			for frame := range b.N {
				scene.Objects[0].Transform = RotateY(float64(frame) * .01).Mul(RotateX(float64(frame) * .006))
				renderer.Render(canvas, scene)
			}
		})
	}
}

func triangleMesh(z float64, color Color) Mesh {
	return Mesh{
		Vertices:  []Vec3{{-1, -.8, z}, {1, -.8, z}, {0, 1, z}},
		Triangles: []Triangle{{A: 0, B: 1, C: 2, Color: color}},
	}
}

func renderScene(t *testing.T, objects []Object, width, height int) *render.Buffer {
	t.Helper()
	buffer := render.NewBuffer(width, height)
	renderer := NewRenderer()
	renderer.Render(render.NewCanvas(buffer), Scene{
		Objects: objects,
		Camera:  Camera{Position: Vec3{Z: 3}, Up: Vec3{Y: 1}, FOVY: Degrees(60), Near: .1, Far: 20},
		Light:   Light{Direction: Vec3{Z: 1}, Ambient: 1},
	})
	return buffer
}

func sizeName(width, height int) string {
	if width == 80 && height == 24 {
		return "80x24"
	}
	return "160x48"
}

func close(left, right float64) bool {
	difference := left - right
	return difference > -1e-9 && difference < 1e-9
}
