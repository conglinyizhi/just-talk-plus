//go:build linux && !no_x11

package overlay

/*
#cgo LDFLAGS: -lwayland-client
#cgo LDFLAGS: -lcairo
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <wayland-client.h>
#include <wayland-client-protocol.h>
#include <cairo/cairo.h>
#include "xdg-shell-client-protocol.h"
#include "wlr-layer-shell-unstable-v1-client-protocol.h"

extern void goOverlayRegistryGlobal(uintptr_t handle, struct wl_registry *registry, uint32_t name, char *interface, uint32_t version);
extern void goOverlayLayerConfigure(uintptr_t handle, struct zwlr_layer_surface_v1 *surface, uint32_t serial, uint32_t width, uint32_t height);
extern void goOverlayLayerClosed(uintptr_t handle, struct zwlr_layer_surface_v1 *surface);

static void draw_wayland_system_text(unsigned char *data, int width, int height, const char *text, double font_size, double x, double y) {
    cairo_surface_t *surface = cairo_image_surface_create_for_data(data, CAIRO_FORMAT_ARGB32, width, height, width * 4);
    if (cairo_surface_status(surface) != CAIRO_STATUS_SUCCESS) return;
    cairo_t *cr = cairo_create(surface);
    cairo_select_font_face(cr, "DejaVu Sans Mono", CAIRO_FONT_SLANT_NORMAL, CAIRO_FONT_WEIGHT_BOLD);
    cairo_set_font_size(cr, font_size);
    cairo_set_source_rgba(cr, 0.96, 0.96, 0.96, 1.0);
    cairo_move_to(cr, x, y);
    cairo_show_text(cr, text);
    cairo_destroy(cr);
    cairo_surface_flush(surface);
    cairo_surface_destroy(surface);
}

static double wayland_system_text_width(const char *text, double font_size) {
    cairo_surface_t *surface = cairo_image_surface_create(CAIRO_FORMAT_ARGB32, 1, 1);
    cairo_t *cr = cairo_create(surface);
    cairo_select_font_face(cr, "DejaVu Sans Mono", CAIRO_FONT_SLANT_NORMAL, CAIRO_FONT_WEIGHT_BOLD);
    cairo_set_font_size(cr, font_size);
    cairo_text_extents_t extents;
    cairo_text_extents(cr, text, &extents);
    double width = extents.x_advance;
    cairo_destroy(cr);
    cairo_surface_destroy(surface);
    return width;
}

static void registry_global_cb(void *data, struct wl_registry *registry, uint32_t name, const char *interface, uint32_t version) {
	goOverlayRegistryGlobal((uintptr_t)data, registry, name, (char*)interface, version);
}

static void registry_remove_cb(void *data, struct wl_registry *registry, uint32_t name) {
}

static const struct wl_registry_listener registry_listener = {
	.global = registry_global_cb,
	.global_remove = registry_remove_cb,
};

static void add_registry_listener(struct wl_registry *registry, uintptr_t handle) {
	wl_registry_add_listener(registry, &registry_listener, (void*)handle);
}

static void layer_configure_cb(void *data, struct zwlr_layer_surface_v1 *surface, uint32_t serial, uint32_t width, uint32_t height) {
	goOverlayLayerConfigure((uintptr_t)data, surface, serial, width, height);
}

static void layer_closed_cb(void *data, struct zwlr_layer_surface_v1 *surface) {
	goOverlayLayerClosed((uintptr_t)data, surface);
}

static const struct zwlr_layer_surface_v1_listener layer_surface_listener = {
	.configure = layer_configure_cb,
	.closed = layer_closed_cb,
};

static void add_layer_surface_listener(struct zwlr_layer_surface_v1 *surface, uintptr_t handle) {
	zwlr_layer_surface_v1_add_listener(surface, &layer_surface_listener, (void*)handle);
}

static void *bind_wl_compositor(struct wl_registry *registry, uint32_t name, uint32_t version) {
	if (version > 4) version = 4;
	return wl_registry_bind(registry, name, &wl_compositor_interface, version);
}

static void *bind_wl_shm(struct wl_registry *registry, uint32_t name, uint32_t version) {
	if (version > 1) version = 1;
	return wl_registry_bind(registry, name, &wl_shm_interface, version);
}

static void *bind_layer_shell(struct wl_registry *registry, uint32_t name, uint32_t version) {
	if (version > 4) version = 4;
	return wl_registry_bind(registry, name, &zwlr_layer_shell_v1_interface, version);
}
*/
import "C"

import (
	"fmt"
	"os"
	"runtime/cgo"
	"strings"
	"sync"
	"unsafe"

	"github.com/conglinyizhi/just-talk-plus/config"
	"golang.org/x/sys/unix"
)

type waylandBackend struct {
	mu           sync.Mutex
	display      *C.struct_wl_display
	registry     *C.struct_wl_registry
	compositor   *C.struct_wl_compositor
	shm          *C.struct_wl_shm
	layerShell   *C.struct_zwlr_layer_shell_v1
	surface      *C.struct_wl_surface
	layerSurface *C.struct_zwlr_layer_surface_v1
	pool         *C.struct_wl_shm_pool
	buffer       *C.struct_wl_buffer
	data         []byte
	file         *os.File
	handle       cgo.Handle
	configured   bool
	closed       bool
	destroyed    bool
	visible      bool
	position     string
	scale        float64
	w            int
	h            int
	margin       int
}

func newWaylandBackend(cfg config.OverlayConfig) (backend, error) {
	display := C.wl_display_connect(nil)
	if display == nil {
		return nil, fmt.Errorf("cannot connect to Wayland display")
	}
	scale := cfg.Scale
	if scale <= 0 {
		scale = 1.0
	}
	b := &waylandBackend{display: display, position: cfg.Position, scale: scale}
	b.margin = b.scaled(baseMargin)
	// 窗口尺寸由共享布局算出：列宽是常量（等宽字体下像素宽只取决于字符数），
	// 所以尺寸恒定，文案长短变化不会改变窗口。这里先量一次定尺寸。
	probe := computeLayout(capsuleContent{Left: padLeft(""), Right: padRight("")}, b.metrics(), b.measureText)
	b.w, b.h = probe.Width, probe.Height
	if b.position == "" {
		b.position = "top-right"
	}
	b.handle = cgo.NewHandle(b)
	b.registry = C.wl_display_get_registry(display)
	C.add_registry_listener(b.registry, C.uintptr_t(b.handle))
	C.wl_display_roundtrip(display)
	if b.compositor == nil {
		b.Close()
		return nil, fmt.Errorf("Wayland compositor global not found")
	}
	if b.shm == nil {
		b.Close()
		return nil, fmt.Errorf("Wayland shm global not found")
	}
	if b.layerShell == nil {
		b.Close()
		return nil, fmt.Errorf("wlr-layer-shell protocol is not supported by this compositor")
	}
	if err := b.createSurface(); err != nil {
		b.Close()
		return nil, err
	}
	if err := b.createBuffer(); err != nil {
		b.Close()
		return nil, err
	}
	return b, nil
}

func (b *waylandBackend) Show(content capsuleContent) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.display == nil || b.closed || b.destroyed {
		return nil
	}
	b.draw(content)
	C.wl_surface_attach(b.surface, b.buffer, 0, 0)
	C.wl_surface_damage_buffer(b.surface, 0, 0, C.int32_t(b.w), C.int32_t(b.h))
	C.wl_surface_commit(b.surface)
	C.wl_display_flush(b.display)
	b.visible = true
	return nil
}

func (b *waylandBackend) Hide() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.display == nil || b.closed || b.destroyed || !b.visible {
		return nil
	}
	clear(b.data)
	C.wl_surface_attach(b.surface, b.buffer, 0, 0)
	C.wl_surface_damage_buffer(b.surface, 0, 0, C.int32_t(b.w), C.int32_t(b.h))
	C.wl_surface_commit(b.surface)
	C.wl_display_flush(b.display)
	b.visible = false
	return nil
}

func (b *waylandBackend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.destroyed {
		return nil
	}
	b.destroyed = true
	b.closed = true
	if b.buffer != nil {
		C.wl_buffer_destroy(b.buffer)
		b.buffer = nil
	}
	if b.pool != nil {
		C.wl_shm_pool_destroy(b.pool)
		b.pool = nil
	}
	if b.data != nil {
		_ = unix.Munmap(b.data)
		b.data = nil
	}
	if b.file != nil {
		_ = b.file.Close()
		b.file = nil
	}
	if b.layerSurface != nil {
		C.zwlr_layer_surface_v1_destroy(b.layerSurface)
		b.layerSurface = nil
	}
	if b.surface != nil {
		C.wl_surface_destroy(b.surface)
		b.surface = nil
	}
	if b.layerShell != nil {
		C.zwlr_layer_shell_v1_destroy(b.layerShell)
		b.layerShell = nil
	}
	if b.shm != nil {
		C.wl_shm_destroy(b.shm)
		b.shm = nil
	}
	if b.compositor != nil {
		C.wl_compositor_destroy(b.compositor)
		b.compositor = nil
	}
	if b.registry != nil {
		C.wl_registry_destroy(b.registry)
		b.registry = nil
	}
	if b.display != nil {
		C.wl_display_disconnect(b.display)
		b.display = nil
	}
	if b.handle != 0 {
		b.handle.Delete()
		b.handle = 0
	}
	return nil
}

func (b *waylandBackend) createSurface() error {
	b.surface = C.wl_compositor_create_surface(b.compositor)
	if b.surface == nil {
		return fmt.Errorf("cannot create Wayland surface")
	}
	ns := C.CString("just-talk-plus-overlay")
	defer C.free(unsafe.Pointer(ns))
	b.layerSurface = C.zwlr_layer_shell_v1_get_layer_surface(
		b.layerShell,
		b.surface,
		nil,
		C.ZWLR_LAYER_SHELL_V1_LAYER_OVERLAY,
		ns,
	)
	if b.layerSurface == nil {
		return fmt.Errorf("cannot create layer-shell surface")
	}
	C.add_layer_surface_listener(b.layerSurface, C.uintptr_t(b.handle))
	C.zwlr_layer_surface_v1_set_size(b.layerSurface, C.uint32_t(b.w), C.uint32_t(b.h))
	C.zwlr_layer_surface_v1_set_anchor(b.layerSurface, C.uint32_t(b.anchor()))
	top, right, bottom, left := b.margins()
	C.zwlr_layer_surface_v1_set_margin(b.layerSurface, C.int32_t(top), C.int32_t(right), C.int32_t(bottom), C.int32_t(left))
	C.zwlr_layer_surface_v1_set_keyboard_interactivity(b.layerSurface, C.ZWLR_LAYER_SURFACE_V1_KEYBOARD_INTERACTIVITY_NONE)
	C.zwlr_layer_surface_v1_set_exclusive_zone(b.layerSurface, 0)
	region := C.wl_compositor_create_region(b.compositor)
	C.wl_surface_set_input_region(b.surface, region)
	C.wl_region_destroy(region)
	C.wl_surface_commit(b.surface)
	C.wl_display_roundtrip(b.display)
	if !b.configured {
		return fmt.Errorf("layer-shell surface was not configured")
	}
	return nil
}

func (b *waylandBackend) createBuffer() error {
	size := b.w * b.h * 4
	f, err := os.CreateTemp("", "just-talk-plus-overlay-*.shm")
	if err != nil {
		return err
	}
	_ = os.Remove(f.Name())
	if err := f.Truncate(int64(size)); err != nil {
		f.Close()
		return err
	}
	data, err := unix.Mmap(int(f.Fd()), 0, size, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		f.Close()
		return err
	}
	b.file = f
	b.data = data
	b.pool = C.wl_shm_create_pool(b.shm, C.int32_t(f.Fd()), C.int32_t(size))
	if b.pool == nil {
		return fmt.Errorf("cannot create Wayland shm pool")
	}
	b.buffer = C.wl_shm_pool_create_buffer(b.pool, 0, C.int32_t(b.w), C.int32_t(b.h), C.int32_t(b.w*4), C.WL_SHM_FORMAT_ARGB8888)
	if b.buffer == nil {
		return fmt.Errorf("cannot create Wayland shm buffer")
	}
	return nil
}

func (b *waylandBackend) draw(content capsuleContent) {
	layout := computeLayout(content, b.metrics(), b.measureText)
	clear(b.data)
	bg := rgba{20, 20, 20, 215}
	ball := rgba{uint8(content.Color.R >> 8), uint8(content.Color.G >> 8), uint8(content.Color.B >> 8), 255}

	// 胶囊底：整块圆角矩形（全圆角时就是标准胶囊）。
	for y := 0; y < b.h; y++ {
		for x := 0; x < b.w; x++ {
			if coverage := waylandRoundedRectCoverage(x, y, b.w, b.h, layout.Radius); coverage > 0 {
				c := bg
				c.a = uint8(uint16(c.a) * uint16(coverage) / 255)
				b.setPixel(x, y, c)
			}
		}
	}
	// 中间大球：状态色，绝对居中。
	b.fillCircleAA(layout.BallX, layout.BallY, layout.BallD/2, ball)
	// 左右两栏文字：列宽恒定，文字长度变化不会移动坐标。
	if layout.LeftText != "" {
		b.drawSystemText(layout.LeftX, layout.TextBaseline, layout.LeftText, layout.FontSize)
	}
	if layout.RightText != "" {
		b.drawSystemText(layout.RightX, layout.TextBaseline, layout.RightText, layout.FontSize)
	}
}

// metrics 返回与缩放相关的布局常量。
func (b *waylandBackend) metrics() capsuleMetrics {
	return capsuleMetrics{
		FontSize:  12 * b.scale,
		Padding:   b.scaled(8),
		Gap:       b.scaled(8),
		BallD:     b.scaled(28),
		BallInset: b.scaled(3),
	}
}

// measureText 用 cairo 按当前字号测量像素宽，供共享布局计算使用。
func (b *waylandBackend) measureText(s string) int {
	cs := C.CString(s)
	defer C.free(unsafe.Pointer(cs))
	return int(C.wayland_system_text_width(cs, C.double(12*b.scale)))
}

func (b *waylandBackend) drawSystemText(x, y int, label string, fontSize float64) {
	text := C.CString(label)
	defer C.free(unsafe.Pointer(text))
	C.draw_wayland_system_text((*C.uchar)(unsafe.Pointer(&b.data[0])), C.int(b.w), C.int(b.h), text, C.double(fontSize), C.double(x), C.double(y))
}

type rgba struct {
	r, g, b, a uint8
}

func (b *waylandBackend) setPixel(x, y int, c rgba) {
	if x < 0 || y < 0 || x >= b.w || y >= b.h {
		return
	}
	i := (y*b.w + x) * 4
	a := uint16(c.a)
	b.data[i+0] = uint8(uint16(c.b) * a / 255)
	b.data[i+1] = uint8(uint16(c.g) * a / 255)
	b.data[i+2] = uint8(uint16(c.r) * a / 255)
	b.data[i+3] = c.a
}

func (b *waylandBackend) blendPixel(x, y int, c rgba, coverage uint8) {
	if x < 0 || y < 0 || x >= b.w || y >= b.h || coverage == 0 {
		return
	}
	i := (y*b.w + x) * 4
	a := uint16(coverage)
	inv := uint16(255 - coverage)
	b.data[i+0] = uint8((uint16(c.b)*a + uint16(b.data[i+0])*inv) / 255)
	b.data[i+1] = uint8((uint16(c.g)*a + uint16(b.data[i+1])*inv) / 255)
	b.data[i+2] = uint8((uint16(c.r)*a + uint16(b.data[i+2])*inv) / 255)
	b.data[i+3] = 255
}

func (b *waylandBackend) fillCircleAA(cx, cy, r int, c rgba) {
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
				b.blendPixel(x, y, c, uint8(inside*255/16))
			}
		}
	}
}

func (b *waylandBackend) drawText(x, y int, s string, scale int, c rgba) {
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
						b.setPixel(x+col*scale+xx, y+row*scale+yy, c)
					}
				}
			}
		}
		x += 6 * scale
	}
}

func (b *waylandBackend) anchor() int {
	switch strings.ToLower(b.position) {
	case "top-left":
		return C.ZWLR_LAYER_SURFACE_V1_ANCHOR_TOP | C.ZWLR_LAYER_SURFACE_V1_ANCHOR_LEFT
	case "top-center":
		return C.ZWLR_LAYER_SURFACE_V1_ANCHOR_TOP
	case "bottom-left":
		return C.ZWLR_LAYER_SURFACE_V1_ANCHOR_BOTTOM | C.ZWLR_LAYER_SURFACE_V1_ANCHOR_LEFT
	case "bottom-center":
		return C.ZWLR_LAYER_SURFACE_V1_ANCHOR_BOTTOM
	case "bottom-right":
		return C.ZWLR_LAYER_SURFACE_V1_ANCHOR_BOTTOM | C.ZWLR_LAYER_SURFACE_V1_ANCHOR_RIGHT
	default:
		return C.ZWLR_LAYER_SURFACE_V1_ANCHOR_TOP | C.ZWLR_LAYER_SURFACE_V1_ANCHOR_RIGHT
	}
}

func (b *waylandBackend) margins() (top, right, bottom, left int) {
	switch strings.ToLower(b.position) {
	case "top-left":
		return b.margin, 0, 0, b.margin
	case "top-center":
		return b.margin, 0, 0, 0
	case "bottom-left":
		return 0, 0, b.margin, b.margin
	case "bottom-center":
		return 0, 0, b.margin, 0
	case "bottom-right":
		return 0, b.margin, b.margin, 0
	default:
		return b.margin, b.margin, 0, 0
	}
}

func (b *waylandBackend) scaled(v int) int {
	n := int(float64(v)*b.scale + 0.5)
	if n < 1 {
		return 1
	}
	return n
}

func waylandRoundedRectCoverage(x, y, w, h, r int) uint8 {
	inside := 0
	const samples = 8
	for sy := 0; sy < samples; sy++ {
		for sx := 0; sx < samples; sx++ {
			if insideWaylandRoundedRectSample(x*samples+sx, y*samples+sy, w*samples, h*samples, r*samples) {
				inside++
			}
		}
	}
	return uint8(inside * 255 / (samples * samples))
}

func insideWaylandRoundedRectSample(x, y, w, h, r int) bool {
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

//export goOverlayRegistryGlobal
func goOverlayRegistryGlobal(handle C.uintptr_t, registry *C.struct_wl_registry, name C.uint32_t, iface *C.char, version C.uint32_t) {
	h := cgo.Handle(handle)
	b, ok := h.Value().(*waylandBackend)
	if !ok {
		return
	}
	switch C.GoString(iface) {
	case "wl_compositor":
		b.compositor = (*C.struct_wl_compositor)(C.bind_wl_compositor(registry, name, version))
	case "wl_shm":
		b.shm = (*C.struct_wl_shm)(C.bind_wl_shm(registry, name, version))
	case "zwlr_layer_shell_v1":
		b.layerShell = (*C.struct_zwlr_layer_shell_v1)(C.bind_layer_shell(registry, name, version))
	}
}

//export goOverlayLayerConfigure
func goOverlayLayerConfigure(handle C.uintptr_t, surface *C.struct_zwlr_layer_surface_v1, serial C.uint32_t, width C.uint32_t, height C.uint32_t) {
	h := cgo.Handle(handle)
	b, ok := h.Value().(*waylandBackend)
	if !ok {
		return
	}
	C.zwlr_layer_surface_v1_ack_configure(surface, serial)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.configured = true
}

//export goOverlayLayerClosed
func goOverlayLayerClosed(handle C.uintptr_t, surface *C.struct_zwlr_layer_surface_v1) {
	h := cgo.Handle(handle)
	b, ok := h.Value().(*waylandBackend)
	if !ok {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
}
