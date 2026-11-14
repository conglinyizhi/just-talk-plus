//go:build linux && !no_x11

package overlay

// #cgo LDFLAGS: -lX11 -lXext -lXinerama -lXrender -lcairo
// #include <X11/Xlib.h>
// #include <X11/Xatom.h>
// #include <X11/Xutil.h>
// #include <X11/extensions/Xinerama.h>
// #include <X11/extensions/Xrender.h>
// #include <X11/extensions/shape.h>
// #include <stdlib.h>
// #include <stdio.h>
// #include <cairo/cairo.h>
//
// static double draw_system_text(unsigned char *data, int width, int height, const char *text, double font_size, double x, double y) {
//     cairo_surface_t *surface = cairo_image_surface_create_for_data(data, CAIRO_FORMAT_ARGB32, width, height, width * 4);
//     if (cairo_surface_status(surface) != CAIRO_STATUS_SUCCESS) return 0;
//     cairo_t *cr = cairo_create(surface);
//     cairo_select_font_face(cr, "DejaVu Sans Mono", CAIRO_FONT_SLANT_NORMAL, CAIRO_FONT_WEIGHT_BOLD);
//     cairo_set_font_size(cr, font_size);
//     cairo_text_extents_t extents;
//     cairo_text_extents(cr, text, &extents);
//     cairo_set_source_rgba(cr, 0.96, 0.96, 0.96, 1.0);
//     cairo_move_to(cr, x, y);
//     cairo_show_text(cr, text);
//     cairo_destroy(cr);
//     cairo_surface_flush(surface);
//     cairo_surface_destroy(surface);
//     return extents.x_advance;
// }
//
// static double system_text_width(const char *text, double font_size) {
//     cairo_surface_t *surface = cairo_image_surface_create(CAIRO_FORMAT_ARGB32, 1, 1);
//     cairo_t *cr = cairo_create(surface);
//     cairo_select_font_face(cr, "DejaVu Sans Mono", CAIRO_FONT_SLANT_NORMAL, CAIRO_FONT_WEIGHT_BOLD);
//     cairo_set_font_size(cr, font_size);
//     cairo_text_extents_t extents;
//     cairo_text_extents(cr, text, &extents);
//     double width = extents.x_advance;
//     cairo_destroy(cr);
//     cairo_surface_destroy(surface);
//     return width;
// }
//
// static void set_override_redirect(Display *dpy, Window win) {
//     XSetWindowAttributes attrs;
//     attrs.override_redirect = True;
//     attrs.save_under = True;
//     XChangeWindowAttributes(dpy, win, CWOverrideRedirect | CWSaveUnder, &attrs);
// }
//
// static void set_empty_input_shape(Display *dpy, Window win) {
//     XShapeCombineRectangles(dpy, win, ShapeInput, 0, 0, NULL, 0, ShapeSet, Unsorted);
// }
//
// static void set_window_type_utility(Display *dpy, Window win) {
//     Atom type = XInternAtom(dpy, "_NET_WM_WINDOW_TYPE", False);
//     Atom utility = XInternAtom(dpy, "_NET_WM_WINDOW_TYPE_UTILITY", False);
//     XChangeProperty(dpy, win, type, XA_ATOM, 32, PropModeReplace, (unsigned char*)&utility, 1);
// }
//
// static int focused_monitor_geometry(Display *dpy, int screen, int *x, int *y, int *w, int *h) {
//     Window focus;
//     int revert;
//     XGetInputFocus(dpy, &focus, &revert);
//     if (focus == None || focus == PointerRoot) return 0;
//
//     XWindowAttributes attrs;
//     if (!XGetWindowAttributes(dpy, focus, &attrs)) return 0;
//     Window child;
//     int wx = 0, wy = 0;
//     if (!XTranslateCoordinates(dpy, focus, RootWindow(dpy, screen), attrs.width / 2, attrs.height / 2, &wx, &wy, &child)) return 0;
//
//     int count = 0;
//     XineramaScreenInfo *screens = XineramaQueryScreens(dpy, &count);
//     if (screens == NULL || count <= 0) {
//         if (screens != NULL) XFree(screens);
//         return 0;
//     }
//     for (int i = 0; i < count; i++) {
//         int sx = screens[i].x_org;
//         int sy = screens[i].y_org;
//         int sw = screens[i].width;
//         int sh = screens[i].height;
//         if (wx >= sx && wx < sx + sw && wy >= sy && wy < sy + sh) {
//             *x = sx; *y = sy; *w = sw; *h = sh;
//             XFree(screens);
//             return 1;
//         }
//     }
//     XFree(screens);
//     return 0;
// }
//
// static Visual *find_argb_visual(Display *dpy, int screen) {
//     XVisualInfo template;
//     template.screen = screen;
//     template.depth = 32;
//     template.class = TrueColor;
//     int n = 0;
//     XVisualInfo *infos = XGetVisualInfo(dpy, VisualScreenMask | VisualDepthMask | VisualClassMask, &template, &n);
//     if (infos == NULL) return NULL;
//     Visual *visual = NULL;
//     for (int i = 0; i < n; i++) {
//         XRenderPictFormat *fmt = XRenderFindVisualFormat(dpy, infos[i].visual);
//         if (fmt != NULL && fmt->type == PictTypeDirect && fmt->direct.alphaMask) {
//             visual = infos[i].visual;
//             break;
//         }
//     }
//     XFree(infos);
//     return visual;
// }
//
// static XImage *create_argb_image(Display *dpy, Visual *visual, int w, int h, char *data) {
//     return XCreateImage(dpy, visual, 32, ZPixmap, 0, data, (unsigned int)w, (unsigned int)h, 32, 0);
// }
//
// static void destroy_ximage(XImage *img) {
//     XDestroyImage(img);
// }
//
// static int compositing_manager_running(Display *dpy, int screen) {
//     char name[32];
//     snprintf(name, sizeof(name), "_NET_WM_CM_S%d", screen);
//     Atom atom = XInternAtom(dpy, name, False);
//     Window owner = XGetSelectionOwner(dpy, atom);
//     return owner != None;
// }
import "C"

import (
	"fmt"
	"os"
	"strings"
	"unsafe"

	"github.com/conglinyizhi/just-talk-plus/config"
)

const (
	basePillW  = 170
	basePillH  = 42
	baseMargin = 28
)

type x11Backend struct {
	dpy      *C.Display
	win      C.Window
	gc       C.GC
	screen   C.int
	visual   *C.Visual
	cmap     C.Colormap
	mask     C.Pixmap
	depth    C.int
	argb     bool
	visible  bool
	position string
	scale    float64
	w        int
	h        int
	margin   int
}

func newX11Backend(cfg config.OverlayConfig) (backend, error) {
	if os.Getenv("DISPLAY") == "" {
		return nil, fmt.Errorf("DISPLAY is not set")
	}
	dpy := C.XOpenDisplay(nil)
	if dpy == nil {
		return nil, fmt.Errorf("cannot open X display")
	}
	scale := cfg.Scale
	if scale <= 0 {
		scale = 1.0
	}
	b := &x11Backend{dpy: dpy, screen: C.XDefaultScreen(dpy), position: cfg.Position, scale: scale}
	b.margin = b.scaled(baseMargin)
	// 窗口尺寸由共享布局给出：列宽是常量，等宽字体下像素宽只取决于字符数，
	// 因此尺寸恒定，文案长短变化不会改变窗口或坐标。
	probe := computeLayout(capsuleContent{}, b.metrics(), b.measureText)
	b.w, b.h = probe.Width, probe.Height
	if b.position == "" {
		b.position = "top-right"
	}
	root := C.XRootWindow(dpy, b.screen)
	b.argb = C.compositing_manager_running(dpy, b.screen) != 0
	if b.argb {
		b.visual = C.find_argb_visual(dpy, b.screen)
		if b.visual == nil {
			b.argb = false
		}
	}
	if b.argb {
		b.depth = 32
		b.cmap = C.XCreateColormap(dpy, root, b.visual, C.AllocNone)
	} else {
		b.depth = C.XDefaultDepth(dpy, b.screen)
		b.visual = C.XDefaultVisual(dpy, b.screen)
		b.cmap = C.XDefaultColormap(dpy, b.screen)
	}
	attrs := C.XSetWindowAttributes{
		colormap:          b.cmap,
		override_redirect: C.True,
		save_under:        C.True,
		background_pixel:  0,
		border_pixel:      0,
	}
	b.win = C.XCreateWindow(
		dpy,
		root,
		0, 0,
		C.uint(b.w), C.uint(b.h),
		0,
		b.depth,
		C.InputOutput,
		b.visual,
		C.CWColormap|C.CWOverrideRedirect|C.CWSaveUnder|C.CWBackPixel|C.CWBorderPixel,
		&attrs,
	)
	if b.win == 0 {
		C.XCloseDisplay(dpy)
		return nil, fmt.Errorf("cannot create overlay window")
	}
	if !b.argb {
		C.set_override_redirect(dpy, b.win)
		b.applyShape()
	}
	C.set_window_type_utility(dpy, b.win)
	b.gc = C.XCreateGC(dpy, C.Drawable(b.win), 0, nil)
	C.XSelectInput(dpy, b.win, 0)
	C.set_empty_input_shape(dpy, b.win)
	b.move()
	C.XFlush(dpy)
	return b, nil
}

func (b *x11Backend) Show(content capsuleContent) error {
	if b.dpy == nil {
		return nil
	}
	b.move()
	b.draw(content)
	if !b.visible {
		C.XMapRaised(b.dpy, b.win)
		b.visible = true
	} else {
		C.XRaiseWindow(b.dpy, b.win)
	}
	C.XFlush(b.dpy)
	return nil
}

func (b *x11Backend) Hide() error {
	if b.dpy == nil || !b.visible {
		return nil
	}
	C.XUnmapWindow(b.dpy, b.win)
	C.XFlush(b.dpy)
	b.visible = false
	return nil
}

func (b *x11Backend) Close() error {
	if b.dpy == nil {
		return nil
	}
	if b.mask != 0 {
		C.XFreePixmap(b.dpy, b.mask)
	}
	if b.gc != nil {
		C.XFreeGC(b.dpy, b.gc)
	}
	if b.win != 0 {
		C.XDestroyWindow(b.dpy, b.win)
	}
	if b.argb && b.cmap != 0 {
		C.XFreeColormap(b.dpy, b.cmap)
	}
	C.XCloseDisplay(b.dpy)
	b.dpy = nil
	return nil
}

func (b *x11Backend) move() {
	monX, monY, monW, monH := b.focusedMonitor()
	x, y := monX+monW-b.w-b.margin, monY+b.margin
	switch strings.ToLower(b.position) {
	case "top-left":
		x, y = monX+b.margin, monY+b.margin
	case "top-center":
		x, y = monX+(monW-b.w)/2, monY+b.margin
	case "bottom-left":
		x, y = monX+b.margin, monY+monH-b.h-b.margin
	case "bottom-center":
		x, y = monX+(monW-b.w)/2, monY+monH-b.h-b.margin
	case "bottom-right":
		x, y = monX+monW-b.w-b.margin, monY+monH-b.h-b.margin
	}
	if x < monX {
		x = monX
	}
	if y < monY {
		y = monY
	}
	C.XMoveWindow(b.dpy, b.win, C.int(x), C.int(y))
}

func (b *x11Backend) focusedMonitor() (x, y, w, h int) {
	w = int(C.XDisplayWidth(b.dpy, b.screen))
	h = int(C.XDisplayHeight(b.dpy, b.screen))
	var cx, cy, cw, ch C.int
	if C.focused_monitor_geometry(b.dpy, b.screen, &cx, &cy, &cw, &ch) != 0 {
		return int(cx), int(cy), int(cw), int(ch)
	}
	return 0, 0, w, h
}

func (b *x11Backend) draw(content capsuleContent) {
	if !b.argb {
		b.drawShape(content)
		return
	}
	layout := computeLayout(content, b.metrics(), b.measureText)
	p := newARGBCanvas(b.w, b.h)
	bg := rgba{20, 20, 20, 215}
	ball := rgba{uint8(content.Color.R >> 8), uint8(content.Color.G >> 8), uint8(content.Color.B >> 8), 255}
	textScale := b.scaled(3)

	// 胶囊底：整块圆角矩形。
	for y := 0; y < b.h; y++ {
		for x := 0; x < b.w; x++ {
			if coverage := roundedRectCoverage(x, y, b.w, b.h, layout.Radius); coverage > 0 {
				c := bg
				c.a = uint8(uint16(c.a) * uint16(coverage) / 255)
				p.setPixel(x, y, c)
			}
		}
	}
	// 中间大球，绝对居中。
	p.fillCircleAA(layout.BallX, layout.BallY, layout.BallD/2, ball)

	// 左右两栏：坐标由布局给定，文字长度不影响位置。
	textTop := layout.TextBaseline - int(layout.FontSize)
	drawColumn := func(text string, x int) {
		if text == "" {
			return
		}
		if p.drawSystemText(x, textTop, text, layout.FontSize) {
			return
		}
		// 系统字体不可用时退回点阵字形，只影响观感，不影响位置。
		drawBitmapText(b, x, textTop, text, textScale)
	}
	drawColumn(layout.LeftText, layout.LeftX)
	drawColumn(layout.RightText, layout.RightX)
	b.putImage(p.data)
}

func (p *argbCanvas) drawSystemText(x, y int, label string, fontSize float64) bool {
	text := C.CString(label)
	defer C.free(unsafe.Pointer(text))
	C.draw_system_text((*C.uchar)(unsafe.Pointer(&p.data[0])), C.int(p.w), C.int(p.h), text, C.double(fontSize), C.double(x), C.double(y+int(fontSize)))
	return true
}

func (p *argbCanvas) systemTextWidth(label string, fontSize float64) int {
	text := C.CString(label)
	defer C.free(unsafe.Pointer(text))
	return int(C.system_text_width(text, C.double(fontSize)))
}

func (b *x11Backend) drawShape(content capsuleContent) {
	// 不支持 ARGB visual 时的降级路径：X11 原生填充，没有圆角与抗锯齿。
	// 布局仍走共享计算，球位置与两栏对齐和主路径一致。
	layout := computeLayout(content, b.metrics(), b.measureText)
	bg := b.alloc(20<<8, 20<<8, 20<<8)
	fg := b.alloc(245<<8, 245<<8, 245<<8)
	ball := b.alloc(content.Color.R, content.Color.G, content.Color.B)
	textScale := b.scaled(3)

	C.XSetForeground(b.dpy, b.gc, bg)
	C.XFillRectangle(b.dpy, C.Drawable(b.win), b.gc, 0, 0, C.uint(b.w), C.uint(b.h))

	C.XSetForeground(b.dpy, b.gc, ball)
	C.XFillArc(b.dpy, C.Drawable(b.win), b.gc,
		C.int(layout.BallX-layout.BallD/2), C.int(layout.BallY-layout.BallD/2),
		C.uint(layout.BallD), C.uint(layout.BallD), 0, 360*64)

	C.XSetForeground(b.dpy, b.gc, fg)
	textH := 7 * textScale
	textTop := layout.BallY - textH/2
	if layout.LeftText != "" {
		drawBitmapText(b, layout.LeftX, textTop, layout.LeftText, textScale)
	}
	if layout.RightText != "" {
		drawBitmapText(b, layout.RightX, textTop, layout.RightText, textScale)
	}
}

// metrics 返回与缩放相关的布局常量。
func (b *x11Backend) metrics() capsuleMetrics {
	return capsuleMetrics{
		FontSize:  14 * b.scale,
		Padding:   b.scaled(10),
		Gap:       b.scaled(8),
		BallD:     b.scaled(40),
		BallInset: b.scaled(4),
	}
}

// measureText 用 cairo 按当前字号测量像素宽，供共享布局计算使用。
func (b *x11Backend) measureText(s string) int {
	canvas := newARGBCanvas(1, 1)
	return canvas.systemTextWidth(s, 14*b.scale)
}

func (b *x11Backend) alloc(r, g, bl uint16) C.ulong {
	cmap := C.XDefaultColormap(b.dpy, b.screen)
	color := C.XColor{red: C.ushort(r), green: C.ushort(g), blue: C.ushort(bl)}
	if C.XAllocColor(b.dpy, cmap, &color) == 0 {
		return C.XWhitePixel(b.dpy, b.screen)
	}
	return color.pixel
}

func (b *x11Backend) applyShape() {
	data := roundedMask(b.w, b.h, b.h/2)
	ptr := (*C.char)(unsafe.Pointer(&data[0]))
	b.mask = C.XCreateBitmapFromData(b.dpy, C.Drawable(b.win), ptr, C.uint(b.w), C.uint(b.h))
	if b.mask == 0 {
		return
	}
	C.XShapeCombineMask(b.dpy, b.win, C.ShapeBounding, 0, 0, b.mask, C.ShapeSet)
}

func (b *x11Backend) putImage(data []byte) {
	if len(data) == 0 {
		return
	}
	ptr := C.CBytes(data)
	img := C.create_argb_image(b.dpy, b.visual, C.int(b.w), C.int(b.h), (*C.char)(ptr))
	if img == nil {
		C.free(ptr)
		return
	}
	C.XPutImage(b.dpy, C.Drawable(b.win), b.gc, img, 0, 0, 0, 0, C.uint(b.w), C.uint(b.h))
	img.data = nil
	C.destroy_ximage(img)
	C.free(ptr)
}

func (b *x11Backend) scaled(v int) int {
	n := int(float64(v)*b.scale + 0.5)
	if n < 1 {
		return 1
	}
	return n
}

type argbCanvas struct {
	w, h int
	data []byte
}

func newARGBCanvas(w, h int) *argbCanvas {
	return &argbCanvas{w: w, h: h, data: make([]byte, w*h*4)}
}

func (p *argbCanvas) setPixel(x, y int, c rgba) {
	if x < 0 || y < 0 || x >= p.w || y >= p.h {
		return
	}
	i := (y*p.w + x) * 4
	a := uint16(c.a)
	p.data[i+0] = uint8(uint16(c.b) * a / 255)
	p.data[i+1] = uint8(uint16(c.g) * a / 255)
	p.data[i+2] = uint8(uint16(c.r) * a / 255)
	p.data[i+3] = c.a
}

func (p *argbCanvas) blendPixel(x, y int, c rgba, coverage uint8) {
	if x < 0 || y < 0 || x >= p.w || y >= p.h || coverage == 0 {
		return
	}
	i := (y*p.w + x) * 4
	srcA := uint16(c.a) * uint16(coverage) / 255
	inv := uint16(255 - srcA)
	p.data[i+0] = uint8((uint16(c.b)*srcA + uint16(p.data[i+0])*inv) / 255)
	p.data[i+1] = uint8((uint16(c.g)*srcA + uint16(p.data[i+1])*inv) / 255)
	p.data[i+2] = uint8((uint16(c.r)*srcA + uint16(p.data[i+2])*inv) / 255)
	p.data[i+3] = uint8(srcA + uint16(p.data[i+3])*inv/255)
}

func (p *argbCanvas) fillCircleAA(cx, cy, r int, c rgba) {
	rr := r * r * 16
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			inside := 0
			for sy := 0; sy < 4; sy++ {
				for sx := 0; sx < 4; sx++ {
					dx := (x-cx)*4 + sx - 1
					dy := (y-cy)*4 + sy - 1
					if dx*dx+dy*dy <= rr {
						inside++
					}
				}
			}
			if inside > 0 {
				p.blendPixel(x, y, c, uint8(inside*255/16))
			}
		}
	}
}

func (p *argbCanvas) drawText(x, y int, s string, scale int, c rgba) {
	for _, r := range strings.ToUpper(s) {
		glyph, ok := glyphs[r]
		if !ok {
			x += 4 * scale
			continue
		}
		for row, bits := range glyph {
			for col := 0; col < 5; col++ {
				if bits&(1<<(4-col)) == 0 {
					continue
				}
				for yy := 0; yy < scale; yy++ {
					for xx := 0; xx < scale; xx++ {
						p.setPixel(x+col*scale+xx, y+row*scale+yy, c)
					}
				}
			}
		}
		x += 6 * scale
	}
}

func drawBitmapText(b *x11Backend, x, y int, s string, scale int) {
	for _, r := range strings.ToUpper(s) {
		glyph, ok := glyphs[r]
		if !ok {
			x += 4 * scale
			continue
		}
		for row, bits := range glyph {
			for col := 0; col < 5; col++ {
				if bits&(1<<(4-col)) == 0 {
					continue
				}
				C.XFillRectangle(
					b.dpy,
					C.Drawable(b.win),
					b.gc,
					C.int(x+col*scale),
					C.int(y+row*scale),
					C.uint(scale),
					C.uint(scale),
				)
			}
		}
		x += 6 * scale
	}
}

func bitmapTextWidth(s string, scale int) int {
	if len(s) == 0 {
		return 0
	}
	return (len(s)*6 - 1) * scale
}

var glyphs = map[rune][7]byte{
	' ': {0, 0, 0, 0, 0, 0, 0},
	'#': {0b01010, 0b11111, 0b01010, 0b01010, 0b11111, 0b01010, 0b01010},
	'-': {0, 0, 0, 0b11111, 0, 0, 0},
	'[': {0b01110, 0b01000, 0b01000, 0b01000, 0b01000, 0b01000, 0b01110},
	']': {0b01110, 0b00010, 0b00010, 0b00010, 0b00010, 0b00010, 0b01110},
	'A': {0b01110, 0b10001, 0b10001, 0b11111, 0b10001, 0b10001, 0b10001},
	'C': {0b01110, 0b10001, 0b10000, 0b10000, 0b10000, 0b10001, 0b01110},
	'D': {0b11110, 0b10001, 0b10001, 0b10001, 0b10001, 0b10001, 0b11110},
	'E': {0b11111, 0b10000, 0b10000, 0b11110, 0b10000, 0b10000, 0b11111},
	'I': {0b11111, 0b00100, 0b00100, 0b00100, 0b00100, 0b00100, 0b11111},
	'N': {0b10001, 0b11001, 0b10101, 0b10011, 0b10001, 0b10001, 0b10001},
	'O': {0b01110, 0b10001, 0b10001, 0b10001, 0b10001, 0b10001, 0b01110},
	'P': {0b11110, 0b10001, 0b10001, 0b11110, 0b10000, 0b10000, 0b10000},
	'R': {0b11110, 0b10001, 0b10001, 0b11110, 0b10100, 0b10010, 0b10001},
	'S': {0b01111, 0b10000, 0b10000, 0b01110, 0b00001, 0b00001, 0b11110},
	'T': {0b11111, 0b00100, 0b00100, 0b00100, 0b00100, 0b00100, 0b00100},
	'W': {0b10001, 0b10001, 0b10001, 0b10101, 0b10101, 0b10101, 0b01010},
}

func roundedRectCoverage(x, y, w, h, r int) uint8 {
	inside := 0
	const samples = 8
	for sy := 0; sy < samples; sy++ {
		for sx := 0; sx < samples; sx++ {
			if insideRoundedRectSample(x*samples+sx, y*samples+sy, w*samples, h*samples, r*samples) {
				inside++
			}
		}
	}
	return uint8(inside * 255 / (samples * samples))
}

func roundedMask(w, h, r int) []byte {
	stride := (w + 7) / 8
	data := make([]byte, stride*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if roundedRectCoverage(x, y, w, h, r) >= 112 {
				data[y*stride+x/8] |= 1 << uint(x%8)
			}
		}
	}
	return data
}

func insideRoundedRectSample(x, y, w, h, r int) bool {
	if x >= r && x < w-r {
		return true
	}
	cx := r
	if x >= w-r {
		cx = w - r - 1
	}
	cy := r
	if y >= h/2 {
		cy = h - r - 1
	}
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= r*r
}
