// Package scene3d renders small, dependency-free 3D scenes into a render.Canvas.
package scene3d

import (
	"math"

	"github.com/GofMan5/lux/render"
)

const defaultRamp = " .:-=+*#%@"

// Vec3 is a three-dimensional vector or point.
type Vec3 struct {
	X float64
	Y float64
	Z float64
}

// V3 is shorthand for constructing a Vec3.
func V3(x, y, z float64) Vec3 { return Vec3{X: x, Y: y, Z: z} }

func (v Vec3) Add(other Vec3) Vec3 { return Vec3{v.X + other.X, v.Y + other.Y, v.Z + other.Z} }
func (v Vec3) Sub(other Vec3) Vec3 { return Vec3{v.X - other.X, v.Y - other.Y, v.Z - other.Z} }
func (v Vec3) Scale(scale float64) Vec3 {
	return Vec3{v.X * scale, v.Y * scale, v.Z * scale}
}
func (v Vec3) Dot(other Vec3) float64 { return v.X*other.X + v.Y*other.Y + v.Z*other.Z }
func (v Vec3) Cross(other Vec3) Vec3 {
	return Vec3{
		X: v.Y*other.Z - v.Z*other.Y,
		Y: v.Z*other.X - v.X*other.Z,
		Z: v.X*other.Y - v.Y*other.X,
	}
}
func (v Vec3) Length() float64 { return math.Sqrt(v.Dot(v)) }
func (v Vec3) Normalize() Vec3 {
	length := v.Length()
	if length == 0 || math.IsNaN(length) || math.IsInf(length, 0) {
		return Vec3{}
	}
	return v.Scale(1 / length)
}

// Mat4 is a row-major 4x4 matrix. A.Mul(B) applies B, then A.
type Mat4 [16]float64

// Identity returns an identity transform.
func Identity() Mat4 { return Mat4{0: 1, 5: 1, 10: 1, 15: 1} }

// Translate returns a translation transform.
func Translate(x, y, z float64) Mat4 {
	matrix := Identity()
	matrix[3], matrix[7], matrix[11] = x, y, z
	return matrix
}

// Scale returns a scale transform.
func Scale(x, y, z float64) Mat4 {
	return Mat4{0: x, 5: y, 10: z, 15: 1}
}

// RotateX returns a rotation around the X axis.
func RotateX(radians float64) Mat4 {
	sine, cosine := math.Sincos(radians)
	return Mat4{0: 1, 5: cosine, 6: -sine, 9: sine, 10: cosine, 15: 1}
}

// RotateY returns a rotation around the Y axis.
func RotateY(radians float64) Mat4 {
	sine, cosine := math.Sincos(radians)
	return Mat4{0: cosine, 2: sine, 5: 1, 8: -sine, 10: cosine, 15: 1}
}

// RotateZ returns a rotation around the Z axis.
func RotateZ(radians float64) Mat4 {
	sine, cosine := math.Sincos(radians)
	return Mat4{0: cosine, 1: -sine, 4: sine, 5: cosine, 10: 1, 15: 1}
}

// Degrees converts degrees to radians.
func Degrees(value float64) float64 { return value * math.Pi / 180 }

// Mul composes two transforms. A.Mul(B) applies B, then A.
func (matrix Mat4) Mul(other Mat4) Mat4 {
	var result Mat4
	for row := range 4 {
		for column := range 4 {
			for index := range 4 {
				result[row*4+column] += matrix[row*4+index] * other[index*4+column]
			}
		}
	}
	return result
}

// TransformPoint transforms a point and performs a perspective divide when needed.
func (matrix Mat4) TransformPoint(point Vec3) Vec3 {
	x := matrix[0]*point.X + matrix[1]*point.Y + matrix[2]*point.Z + matrix[3]
	y := matrix[4]*point.X + matrix[5]*point.Y + matrix[6]*point.Z + matrix[7]
	z := matrix[8]*point.X + matrix[9]*point.Y + matrix[10]*point.Z + matrix[11]
	w := matrix[12]*point.X + matrix[13]*point.Y + matrix[14]*point.Z + matrix[15]
	if w != 0 && w != 1 {
		return Vec3{x / w, y / w, z / w}
	}
	return Vec3{x, y, z}
}

// TransformVector transforms a direction without applying translation.
func (matrix Mat4) TransformVector(vector Vec3) Vec3 {
	return Vec3{
		matrix[0]*vector.X + matrix[1]*vector.Y + matrix[2]*vector.Z,
		matrix[4]*vector.X + matrix[5]*vector.Y + matrix[6]*vector.Z,
		matrix[8]*vector.X + matrix[9]*vector.Y + matrix[10]*vector.Z,
	}
}

// LookAt returns a right-handed view matrix looking from eye toward target.
func LookAt(eye, target, up Vec3) Mat4 {
	forward := target.Sub(eye).Normalize()
	if forward == (Vec3{}) {
		forward = Vec3{Z: -1}
	}
	if up.Normalize() == (Vec3{}) {
		up = Vec3{Y: 1}
	}
	right := forward.Cross(up).Normalize()
	if right == (Vec3{}) {
		up = Vec3{Z: 1}
		right = forward.Cross(up).Normalize()
	}
	vertical := right.Cross(forward)
	return Mat4{
		right.X, right.Y, right.Z, -right.Dot(eye),
		vertical.X, vertical.Y, vertical.Z, -vertical.Dot(eye),
		-forward.X, -forward.Y, -forward.Z, forward.Dot(eye),
		0, 0, 0, 1,
	}
}

// Perspective returns a right-handed OpenGL-style perspective matrix.
func Perspective(fovY, aspect, near, far float64) Mat4 {
	if fovY <= 0 || fovY >= math.Pi || aspect <= 0 || near <= 0 || far <= near {
		return Mat4{}
	}
	focal := 1 / math.Tan(fovY/2)
	return Mat4{
		0:  focal / aspect,
		5:  focal,
		10: (far + near) / (near - far),
		11: 2 * far * near / (near - far),
		14: -1,
	}
}

func (matrix Mat4) zero() bool { return matrix == (Mat4{}) }

// Color is an RGB surface color. Construct it with RGB; zero means use the
// renderer's DefaultColor, so RGB(0, 0, 0) remains a usable explicit black.
type Color uint32

const colorSet Color = 1 << 24

// RGB returns an explicit RGB surface color.
func RGB(red, green, blue uint8) Color {
	return colorSet | Color(red)<<16 | Color(green)<<8 | Color(blue)
}

func (color Color) channels(fallback Color) (red, green, blue uint8) {
	if color&colorSet == 0 {
		color = fallback
	}
	return uint8(color >> 16), uint8(color >> 8), uint8(color)
}

// Triangle indexes three counter-clockwise vertices. Counter-clockwise means
// the visible side when viewed from outside a closed mesh.
type Triangle struct {
	A     int
	B     int
	C     int
	Color Color
}

// Mesh is indexed triangle geometry.
type Mesh struct {
	Vertices  []Vec3
	Triangles []Triangle
}

// Cube returns an outward-wound cube centered at the origin.
func Cube(size float64) Mesh {
	half := math.Abs(size) / 2
	vertices := []Vec3{
		{-half, -half, -half}, {half, -half, -half},
		{half, half, -half}, {-half, half, -half},
		{-half, -half, half}, {half, -half, half},
		{half, half, half}, {-half, half, half},
	}
	face := func(a, b, c, d int, color Color) []Triangle {
		return []Triangle{{a, b, c, color}, {a, c, d, color}}
	}
	triangles := make([]Triangle, 0, 12)
	triangles = append(triangles, face(4, 5, 6, 7, RGB(239, 92, 106))...)  // front
	triangles = append(triangles, face(1, 0, 3, 2, RGB(94, 170, 255))...)  // back
	triangles = append(triangles, face(0, 4, 7, 3, RGB(171, 112, 255))...) // left
	triangles = append(triangles, face(5, 1, 2, 6, RGB(255, 181, 71))...)  // right
	triangles = append(triangles, face(3, 7, 6, 2, RGB(102, 220, 173))...) // top
	triangles = append(triangles, face(0, 1, 5, 4, RGB(68, 190, 203))...)  // bottom
	return Mesh{Vertices: vertices, Triangles: triangles}
}

// Object places a mesh in the scene. The zero Transform is treated as Identity.
type Object struct {
	Mesh      Mesh
	Transform Mat4
}

// Camera is a perspective camera. Zero fields receive practical defaults:
// position (0,0,3), target origin, up +Y, 60 degree FOV, near .1, far 100.
type Camera struct {
	Position Vec3
	Target   Vec3
	Up       Vec3
	FOVY     float64
	Near     float64
	Far      float64
}

// Light is a flat directional light. Direction points from the surface toward
// the light. A zero Light defaults to a soft key light.
type Light struct {
	Direction Vec3
	Ambient   float64
	Diffuse   float64
}

// Scene is everything needed for one frame.
type Scene struct {
	Objects []Object
	Camera  Camera
	Light   Light
}

// ColorMode chooses how shaded pixels map to terminal styles.
type ColorMode uint8

const (
	// TrueColor emits RGB foreground/background styles and half-block pixels.
	TrueColor ColorMode = iota
	// ANSI16 quantizes foreground/background styles to the standard 16 colors.
	ANSI16
	// Monochrome emits a luminance ramp using the renderer's default color.
	Monochrome
)

// Renderer owns reusable depth, pixel, transform and ramp buffers. It is not
// safe for concurrent use; one renderer per animated viewport avoids locking.
type Renderer struct {
	ColorMode    ColorMode
	Background   Color
	DefaultColor Color
	Attrs        render.Attr
	// CellAspect is terminal cell width divided by height. Zero defaults to .5.
	CellAspect float64
	// Ramp is an optional one-column, dark-to-bright monochrome ramp.
	Ramp string
	// DoubleSided disables back-face culling when true.
	DoubleSided bool

	width      int
	height     int
	depth      []float64
	pixels     []pixel
	world      []Vec3
	view       []Vec3
	rampSource string
	ramp       []string
}

type pixel struct {
	red   uint8
	green uint8
	blue  uint8
	luma  float64
	hit   bool
}

type projected struct {
	x        float64
	y        float64
	inverseZ float64
}

// NewRenderer returns a reusable true-color renderer.
func NewRenderer() *Renderer {
	return &Renderer{
		ColorMode:    TrueColor,
		Background:   RGB(0, 0, 0),
		DefaultColor: RGB(225, 230, 240),
		CellAspect:   .5,
	}
}

// Render replaces canvas with the rendered scene. It is a no-op for nil or
// zero-sized canvases and safely skips malformed/non-finite geometry.
func (renderer *Renderer) Render(canvas *render.Canvas, scene Scene) {
	if renderer == nil || canvas == nil {
		return
	}
	cellWidth, cellHeight := canvas.Size()
	if cellWidth <= 0 || cellHeight <= 0 || cellHeight > int(^uint(0)>>1)/2 {
		return
	}
	rasterHeight := cellHeight * 2
	if cellWidth > int(^uint(0)>>1)/rasterHeight {
		return
	}
	renderer.reset(cellWidth, rasterHeight)

	camera := normalizedCamera(scene.Camera)
	light := normalizedLight(scene.Light)
	viewMatrix := LookAt(camera.Position, camera.Target, camera.Up)
	aspect := float64(cellWidth) * renderer.cellAspect() / float64(cellHeight)
	focal := 1 / math.Tan(camera.FOVY/2)

	for objectIndex := range scene.Objects {
		object := &scene.Objects[objectIndex]
		transform := object.Transform
		if transform.zero() {
			transform = Identity()
		}
		renderer.transform(object.Mesh.Vertices, transform, viewMatrix)
		for triangleIndex := range object.Mesh.Triangles {
			triangle := object.Mesh.Triangles[triangleIndex]
			if !validTriangle(triangle, len(renderer.world)) {
				continue
			}
			worldA, worldB, worldC := renderer.world[triangle.A], renderer.world[triangle.B], renderer.world[triangle.C]
			normal := worldB.Sub(worldA).Cross(worldC.Sub(worldA)).Normalize()
			if normal == (Vec3{}) || !finiteVec(normal) {
				continue
			}
			if !renderer.DoubleSided && normal.Dot(camera.Position.Sub(worldA)) <= 0 {
				continue
			}
			brightness := clamp(light.Ambient+light.Diffuse*math.Max(0, normal.Dot(light.Direction)), 0, 1)
			color := renderer.shaded(triangle.Color, brightness)
			clipped, count := clipNear([3]Vec3{
				renderer.view[triangle.A], renderer.view[triangle.B], renderer.view[triangle.C],
			}, camera.Near)
			for index := 1; index+1 < count; index++ {
				a, okA := project(clipped[0], cellWidth, rasterHeight, aspect, focal)
				b, okB := project(clipped[index], cellWidth, rasterHeight, aspect, focal)
				c, okC := project(clipped[index+1], cellWidth, rasterHeight, aspect, focal)
				if okA && okB && okC {
					renderer.rasterize(a, b, c, color, camera.Near, camera.Far)
				}
			}
		}
	}
	renderer.paint(canvas, cellHeight)
}

func normalizedCamera(camera Camera) Camera {
	if !finiteVec(camera.Position) {
		camera.Position = Vec3{Z: 3}
	}
	if !finiteVec(camera.Target) {
		camera.Target = Vec3{}
	}
	if !finiteVec(camera.Up) {
		camera.Up = Vec3{Y: 1}
	}
	if camera == (Camera{}) {
		camera.Position = Vec3{Z: 3}
	}
	if camera.Position == camera.Target {
		camera.Target = camera.Position.Add(Vec3{Z: -1})
	}
	if camera.Up.Normalize() == (Vec3{}) {
		camera.Up = Vec3{Y: 1}
	}
	if !finite(camera.FOVY) || camera.FOVY <= 0 || camera.FOVY >= math.Pi {
		camera.FOVY = Degrees(60)
	}
	if camera.Near <= 0 || !finite(camera.Near) {
		camera.Near = .1
	}
	if camera.Far <= camera.Near || !finite(camera.Far) {
		camera.Far = 100
	}
	return camera
}

func normalizedLight(light Light) Light {
	if light == (Light{}) {
		light = Light{Direction: Vec3{X: -.4, Y: .7, Z: 1}, Ambient: .18, Diffuse: .82}
	}
	light.Direction = light.Direction.Normalize()
	if light.Direction == (Vec3{}) {
		light.Direction = Vec3{Z: 1}
	}
	light.Ambient = clamp(light.Ambient, 0, 1)
	light.Diffuse = clamp(light.Diffuse, 0, 1)
	return light
}

func (renderer *Renderer) cellAspect() float64 {
	if renderer.CellAspect <= 0 || !finite(renderer.CellAspect) {
		return .5
	}
	return renderer.CellAspect
}

func (renderer *Renderer) reset(width, height int) {
	length := width * height
	if cap(renderer.depth) < length {
		renderer.depth = make([]float64, length)
		renderer.pixels = make([]pixel, length)
	} else {
		renderer.depth = renderer.depth[:length]
		renderer.pixels = renderer.pixels[:length]
		clear(renderer.depth)
		clear(renderer.pixels)
	}
	renderer.width, renderer.height = width, height
}

func (renderer *Renderer) transform(vertices []Vec3, model, view Mat4) {
	if cap(renderer.world) < len(vertices) {
		renderer.world = make([]Vec3, len(vertices))
		renderer.view = make([]Vec3, len(vertices))
	} else {
		renderer.world = renderer.world[:len(vertices)]
		renderer.view = renderer.view[:len(vertices)]
	}
	for index, vertex := range vertices {
		renderer.world[index] = model.TransformPoint(vertex)
		renderer.view[index] = view.TransformPoint(renderer.world[index])
	}
}

func validTriangle(triangle Triangle, vertices int) bool {
	return triangle.A >= 0 && triangle.A < vertices &&
		triangle.B >= 0 && triangle.B < vertices &&
		triangle.C >= 0 && triangle.C < vertices
}

func clipNear(input [3]Vec3, near float64) (output [4]Vec3, count int) {
	previous := input[2]
	previousInside := previous.Z <= -near
	for _, current := range input {
		currentInside := current.Z <= -near
		if currentInside != previousInside {
			delta := current.Sub(previous)
			if delta.Z != 0 {
				ratio := (-near - previous.Z) / delta.Z
				output[count] = previous.Add(delta.Scale(ratio))
				count++
			}
		}
		if currentInside {
			output[count] = current
			count++
		}
		previous, previousInside = current, currentInside
	}
	return output, count
}

func project(point Vec3, width, height int, aspect, focal float64) (projected, bool) {
	depth := -point.Z
	if depth <= 0 || aspect <= 0 || !finiteVec(point) {
		return projected{}, false
	}
	ndcX := point.X * focal / (depth * aspect)
	ndcY := point.Y * focal / depth
	result := projected{
		x:        (ndcX + 1) * .5 * float64(width),
		y:        (1 - ndcY) * .5 * float64(height),
		inverseZ: 1 / depth,
	}
	return result, finite(result.x) && finite(result.y) && finite(result.inverseZ)
}

func (renderer *Renderer) rasterize(a, b, c projected, color pixel, near, far float64) {
	area := edge(a.x, a.y, b.x, b.y, c.x, c.y)
	if math.Abs(area) < 1e-12 || !finite(area) {
		return
	}
	minimumX := max(0, int(math.Floor(math.Max(0, math.Min(a.x, math.Min(b.x, c.x))))))
	maximumX := min(renderer.width-1, int(math.Ceil(math.Min(float64(renderer.width-1), math.Max(a.x, math.Max(b.x, c.x))))))
	minimumY := max(0, int(math.Floor(math.Max(0, math.Min(a.y, math.Min(b.y, c.y))))))
	maximumY := min(renderer.height-1, int(math.Ceil(math.Min(float64(renderer.height-1), math.Max(a.y, math.Max(b.y, c.y))))))
	if minimumX > maximumX || minimumY > maximumY {
		return
	}
	minimumDepth, maximumDepth := 1/far, 1/near
	for y := minimumY; y <= maximumY; y++ {
		py := float64(y) + .5
		for x := minimumX; x <= maximumX; x++ {
			px := float64(x) + .5
			weightA := edge(b.x, b.y, c.x, c.y, px, py) / area
			weightB := edge(c.x, c.y, a.x, a.y, px, py) / area
			weightC := 1 - weightA - weightB
			if weightA < -1e-9 || weightB < -1e-9 || weightC < -1e-9 {
				continue
			}
			inverseZ := weightA*a.inverseZ + weightB*b.inverseZ + weightC*c.inverseZ
			if inverseZ < minimumDepth || inverseZ > maximumDepth {
				continue
			}
			index := y*renderer.width + x
			if inverseZ > renderer.depth[index] {
				renderer.depth[index] = inverseZ
				renderer.pixels[index] = color
			}
		}
	}
}

func edge(ax, ay, bx, by, px, py float64) float64 {
	return (px-ax)*(by-ay) - (py-ay)*(bx-ax)
}

func (renderer *Renderer) shaded(color Color, brightness float64) pixel {
	fallback := renderer.DefaultColor
	if fallback&colorSet == 0 {
		fallback = RGB(225, 230, 240)
	}
	red, green, blue := color.channels(fallback)
	shadedRed := uint8(math.Round(float64(red) * brightness))
	shadedGreen := uint8(math.Round(float64(green) * brightness))
	shadedBlue := uint8(math.Round(float64(blue) * brightness))
	return pixel{
		red: shadedRed, green: shadedGreen, blue: shadedBlue,
		luma: clamp((.2126*float64(shadedRed)+.7152*float64(shadedGreen)+.0722*float64(shadedBlue))/255, 0, 1),
		hit:  true,
	}
}

func (renderer *Renderer) paint(canvas *render.Canvas, cellHeight int) {
	background := renderer.Background
	if background&colorSet == 0 {
		background = RGB(0, 0, 0)
	}
	backgroundColor := renderer.outputColor(renderer.shaded(background, 1))
	if renderer.ColorMode == Monochrome {
		renderer.paintMonochrome(canvas, cellHeight, backgroundColor)
		return
	}
	for y := range cellHeight {
		for x := range renderer.width {
			top := renderer.pixels[(y*2)*renderer.width+x]
			bottom := renderer.pixels[(y*2+1)*renderer.width+x]
			style := render.Style{Background: backgroundColor, Attrs: renderer.Attrs}
			switch {
			case top.hit:
				style.Foreground = renderer.outputColor(top)
				if bottom.hit {
					style.Background = renderer.outputColor(bottom)
				}
				canvas.Put(x, y, "\u2580", style)
			case bottom.hit:
				style.Foreground = renderer.outputColor(bottom)
				canvas.Put(x, y, "\u2584", style)
			default:
				canvas.Put(x, y, " ", style)
			}
		}
	}
}

func (renderer *Renderer) paintMonochrome(canvas *render.Canvas, cellHeight int, background render.Color) {
	ramp := renderer.luminanceRamp()
	foreground := renderer.DefaultColor
	if foreground&colorSet == 0 {
		foreground = RGB(225, 230, 240)
	}
	red, green, blue := foreground.channels(foreground)
	style := render.Style{
		Foreground: renderer.outputColor(pixel{red: red, green: green, blue: blue, hit: true}),
		Background: background,
		Attrs:      renderer.Attrs,
	}
	for y := range cellHeight {
		for x := range renderer.width {
			top := renderer.pixels[(y*2)*renderer.width+x]
			bottom := renderer.pixels[(y*2+1)*renderer.width+x]
			if !top.hit && !bottom.hit {
				canvas.Put(x, y, " ", style)
				continue
			}
			luminance := (top.luma + bottom.luma) / 2
			index := int(math.Round(luminance * float64(len(ramp)-1)))
			canvas.Put(x, y, ramp[min(max(index, 0), len(ramp)-1)], style)
		}
	}
}

func (renderer *Renderer) luminanceRamp() []string {
	source := renderer.Ramp
	if source == "" {
		source = defaultRamp
	}
	if source == renderer.rampSource && len(renderer.ramp) > 1 {
		return renderer.ramp
	}
	renderer.rampSource = source
	renderer.ramp = renderer.ramp[:0]
	for _, character := range source {
		grapheme := string(character)
		if render.GraphemeWidth(grapheme) == 1 {
			renderer.ramp = append(renderer.ramp, grapheme)
		}
	}
	if len(renderer.ramp) < 2 {
		renderer.ramp = []string{" ", "#"}
	}
	return renderer.ramp
}

func (renderer *Renderer) outputColor(value pixel) render.Color {
	if renderer.ColorMode == ANSI16 {
		return render.ANSIColor(nearestANSI(value.red, value.green, value.blue))
	}
	return render.RGBColor(value.red, value.green, value.blue)
}

var ansiPalette = [16][3]uint8{
	{0, 0, 0}, {128, 0, 0}, {0, 128, 0}, {128, 128, 0},
	{0, 0, 128}, {128, 0, 128}, {0, 128, 128}, {192, 192, 192},
	{128, 128, 128}, {255, 0, 0}, {0, 255, 0}, {255, 255, 0},
	{0, 0, 255}, {255, 0, 255}, {0, 255, 255}, {255, 255, 255},
}

func nearestANSI(red, green, blue uint8) uint8 {
	best, bestDistance := 0, math.MaxInt
	for index, candidate := range ansiPalette {
		deltaRed := int(red) - int(candidate[0])
		deltaGreen := int(green) - int(candidate[1])
		deltaBlue := int(blue) - int(candidate[2])
		distance := deltaRed*deltaRed + deltaGreen*deltaGreen + deltaBlue*deltaBlue
		if distance < bestDistance {
			best, bestDistance = index, distance
		}
	}
	return uint8(best)
}

func finite(value float64) bool  { return !math.IsNaN(value) && !math.IsInf(value, 0) }
func finiteVec(vector Vec3) bool { return finite(vector.X) && finite(vector.Y) && finite(vector.Z) }
func clamp(value, minimum, maximum float64) float64 {
	return math.Min(maximum, math.Max(minimum, value))
}
