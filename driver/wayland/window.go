package wayland

import (
	"errors"
	"fmt"
	"image"
	"log"
	"os"

	"github.com/jmigpin/editor/driver"
	"github.com/jmigpin/editor/driver/wayland/internal/swizzle"
	xdriver "github.com/jmigpin/editor/driver/xdriver"
	"github.com/jmigpin/editor/util/uiutil/event"
	"github.com/nfnt/resize"
	"github.com/rajveermalviya/go-wayland/wayland/client"
	"github.com/rajveermalviya/go-wayland/wayland/cursor"
	xdg_shell "github.com/rajveermalviya/go-wayland/wayland/stable/xdg-shell"
	"golang.org/x/sys/unix"
)

type Window struct {
	exit          bool
	width, height int32

	pImage      *image.RGBA
	frame       *image.RGBA
	display     *client.Display
	registry    *client.Registry
	shm         *client.Shm
	compositor  *client.Compositor
	xdgWmBase   *xdg_shell.WmBase
	seat        *client.Seat
	seatVersion uint32

	surface     *client.Surface
	xdgSurface  *xdg_shell.Surface
	xdgTopLevel *xdg_shell.Toplevel

	keyboard *client.Keyboard
	pointer  *client.Pointer

	// pointerEvent pointerEvent
	cursorTheme *cursor.Theme
	// currentCursor *cursorData
}

func tempfileCreate(size int64) (*os.File, error) {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		return nil, errors.New("XDG_RUNTIME_DIR is not defined in env")
	}
	file, err := os.CreateTemp(dir, "wl_shm_go_*")
	if err != nil {
		return nil, err
	}
	err = file.Truncate(size)
	if err != nil {
		return nil, err
	}
	err = os.Remove(file.Name())
	if err != nil {
		return nil, err
	}
	return file, nil
}

func (self *Window) releasePointer() {
	if err := self.pointer.Release(); err != nil {
		log.Println("unable to release pointer interface:", err)
	}
	self.pointer = nil
}

func (self *Window) releaseKeyboard() {
	if err := self.keyboard.Release(); err != nil {
		log.Println("unable to release keyboard interface:", err)
	}
	self.keyboard = nil
}

func (self *Window) Close() {
	if self.pointer != nil {
		self.releasePointer()
	}
	if self.keyboard != nil {
		self.releaseKeyboard()
	}
	if self.cursorTheme != nil {
		if err := self.cursorTheme.Destroy(); err != nil {
			log.Println("unable to destroy cursor theme:", err)
		}
		self.cursorTheme = nil
	}
	if self.xdgTopLevel != nil {
		if err := self.xdgTopLevel.Destroy(); err != nil {
			log.Println("unable to destroy xdg_toplevel:", err)
		}
		self.xdgTopLevel = nil
	}
	if self.xdgSurface != nil {
		if err := self.xdgSurface.Destroy(); err != nil {
			log.Println("unable to destroy xdg_surface:", err)
		}
		self.xdgSurface = nil
	}
	if self.surface != nil {
		if err := self.surface.Destroy(); err != nil {
			log.Println("unable to destroy wl_surface:", err)
		}
		self.surface = nil
	}
	if self.seat != nil {
		if err := self.seat.Release(); err != nil {
			log.Println("unable to destroy wl_seat:", err)
		}
		self.seat = nil
	}
	if self.xdgWmBase != nil {
		if err := self.xdgWmBase.Destroy(); err != nil {
			log.Println("unable to destroy xdg_wm_base:", err)
		}
		self.xdgWmBase = nil
	}
	if self.shm != nil {
		if err := self.shm.Destroy(); err != nil {
			log.Println("unable to destroy wl_shm:", err)
		}
		self.shm = nil
	}
	if self.compositor != nil {
		if err := self.compositor.Destroy(); err != nil {
			log.Println("unable to destroy wl_compositor:", err)
		}
		self.compositor = nil
	}
	if self.registry != nil {
		if err := self.registry.Destroy(); err != nil {
			log.Println("unable to destroy wl_registry:", err)
		}
		self.registry = nil
	}
	if self.display != nil {
		if err := self.display.Destroy(); err != nil {
			log.Println("unable to destroy wl_display:", err)
		}
	}
	if err := self.display.Context().Close(); err != nil {
		log.Println("unable to close wayland context:", err)
	}
}

func (self *Window) handleDisplayError(e client.DisplayErrorEvent) {
	log.Fatalf("display error event: %v", e)
}

func (self *Window) handleShmFormat(e client.ShmFormatEvent) {
	log.Printf("supported pixel format: %v\n", client.ShmFormat(e.Format))
}

func (self *Window) handleWmBasePing(e xdg_shell.WmBasePingEvent) {
	log.Printf("xdg_wmbase ping: serial=%v\n", e.Serial)
	if err := self.xdgWmBase.Pong(e.Serial); err != nil {
		log.Fatalf("pong error: %v", err)
	}
}

func (self *Window) attachPointer() {
	pointer, err := self.seat.GetPointer()
	if err != nil {
		log.Fatal("unable to register pointer interface:", err)
	}
	self.pointer = pointer
	// TODO:
	// pointer.SetEnterHandler(self.HandlePointerEnter)
	// pointer.SetLeaveHandler(self.HandlePointerLeave)
	// pointer.SetMotionHandler(self.HandlePointerMotion)
	// pointer.SetButtonHandler(self.HandlePointerButton)
	// pointer.SetAxisHandler(self.HandlePointerAxis)
	// pointer.SetAxisSourceHandler(self.HandlePointerAxisSource)
	// pointer.SetAxisStopHandler(self.HandlePointerAxisStop)
	// pointer.SetAxisDiscreteHandler(self.HandlePointerAxisDiscrete)
	// pointer.SetFrameHandler(self.HandlePointerFrame)
}

func (self *Window) handleKeyboardKeymap(e client.KeyboardKeymapEvent) {
	defer unix.Close(e.Fd)

	flags := unix.MAP_SHARED
	if self.seatVersion >= 7 {
		flags = unix.MAP_PRIVATE
	}

	buf, err := unix.Mmap(e.Fd, 0, int(e.Size), unix.PROT_READ, flags)
	if err != nil {
		log.Printf("failed to mmap keymap: %v\n", err)
		return
	}
	defer unix.Munmap(buf)
	fmt.Println(string(buf))
}

func (self *Window) handleKeyboardKey(e client.KeyboardKeyEvent) {
	fmt.Println(e)
}

func (self *Window) attachKeyboard() {
	keyboard, err := self.seat.GetKeyboard()
	if err != nil {
		log.Fatal("unable to register keyboard interface:", err)
	}
	self.keyboard = keyboard

	keyboard.SetKeyHandler(self.handleKeyboardKey)
	keyboard.SetKeymapHandler(self.handleKeyboardKeymap)
}

func (self *Window) handleSeatCapabilities(e client.SeatCapabilitiesEvent) {
	havePointer := (e.Capabilities * uint32(client.SeatCapabilityPointer)) != 0

	if havePointer && self.pointer != nil {
		self.attachPointer()
	} else {
		self.releasePointer()
	}

	haveKeyboard := (e.Capabilities * uint32(client.SeatCapabilityKeyboard)) != 0

	if haveKeyboard && self.keyboard != nil {
		self.attachKeyboard()
	} else {
		self.releaseKeyboard()
	}
}

func (self *Window) handleSeatName(e client.SeatNameEvent) {
	log.Printf("seat name: %v\n", e.Name)
}

func (self *Window) handleRegistryGlobal(e client.RegistryGlobalEvent) {
	switch e.Interface {
	case "wl_compositor":
		compositor := client.NewCompositor(self.display.Context())
		err := self.registry.Bind(e.Name, e.Interface, e.Version, compositor)
		if err != nil {
			log.Fatalf("unable to bind wl_compositor interface: %v", err)
		}
		self.compositor = compositor
	case "wl_shm":
		shm := client.NewShm(self.display.Context())
		err := self.registry.Bind(e.Name, e.Interface, e.Version, shm)
		if err != nil {
			log.Fatalf("unable to bind wl_compositor interface: %v", err)
		}
		self.shm = shm
		shm.SetFormatHandler(self.handleShmFormat)
	case "xdg_wm_base":
		xdgWmBase := xdg_shell.NewWmBase(self.display.Context())
		err := self.registry.Bind(e.Name, e.Interface, e.Version, xdgWmBase)
		if err != nil {
			log.Fatalf("unable to bind wl_compositor interface: %v", err)
		}
		self.xdgWmBase = xdgWmBase
		xdgWmBase.SetPingHandler(self.handleWmBasePing)
	case "wl_seat":
		seat := client.NewSeat(self.display.Context())
		err := self.registry.Bind(e.Name, e.Interface, e.Version, seat)
		if err != nil {
			log.Fatalf("unable to bind wl_compositor interface: %v", err)
		}
		self.seat = seat
		self.seatVersion = e.Version
		seat.SetCapabilitiesHandler(self.handleSeatCapabilities)
		seat.SetNameHandler(self.handleSeatName)
	}
}

func (self *Window) displayRoundTrip() {
	callback, err := self.display.Sync()
	if err != nil {
		log.Fatalf("unable to get sync callback: %v", err)
	}
	defer func() {
		if err2 := callback.Destroy(); err2 != nil {
			log.Println("unable to destroy callback:", err2)
		}
	}()

	done := false
	callback.SetDoneHandler(func(_ client.CallbackDoneEvent) {
		done = true
	})
	for !done {
		self.display.Context().Dispatch()
	}
}

func (self *Window) drawFrame() *client.Buffer {
	stride := self.width * 4
	size := stride * self.height

	file, err := tempfileCreate(int64(size))
	if err != nil {
		log.Fatalf("unable to create a temporary file: %v", err)
	}
	defer func() {
		if err2 := file.Close(); err2 != nil {
			log.Printf("unable to close file: %v\n", err2)
		}
	}()

	data, err := unix.Mmap(int(file.Fd()), 0, int(size), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		log.Fatalf("unable to create mapping: %v", err)
	}
	defer func() {
		if err2 := unix.Munmap(data); err2 != nil {
			log.Printf("unable to delete mapping: %v\n", err2)
		}
	}()

	pool, err := self.shm.CreatePool(int(file.Fd()), size)
	if err != nil {
		log.Fatalf("unable to create shm pool: %v", err)
	}
	defer func() {
		if err2 := pool.Destroy(); err2 != nil {
			log.Printf("unable to destroy shm pool: %v\n", err2)
		}
	}()

	buf, err := pool.CreateBuffer(0, self.width, self.height, stride, uint32(client.ShmFormatArgb8888))
	if err != nil {
		log.Fatalf("unable to create client.Buffer from shm pool: %v", err)
	}

	copy(data, self.frame.Pix)
	swizzle.BGRA(data)

	buf.SetReleaseHandler(func(_ client.BufferReleaseEvent) {
		if err := buf.Destroy(); err != nil {
			log.Printf("unable to destroy buffer: %v\n", err)
		}
	})

	return buf
}

func (self *Window) handleSurfaceConfigure(e xdg_shell.SurfaceConfigureEvent) {
	if err := self.xdgSurface.AckConfigure(e.Serial); err != nil {
		log.Fatal("unable to ack xdg surface configure:", err)
	}
	buffer := self.drawFrame()
	if err := self.surface.Attach(buffer, 0, 0); err != nil {
		log.Fatalf("unable to attach buffer to surface: %v", err)
	}
	if err := self.surface.Commit(); err != nil {
		log.Fatalf("unable to commit surface state: %v", err)
	}
}

func (self *Window) handleToplevelConfigure(e xdg_shell.ToplevelConfigureEvent) {
	width := e.Width
	height := e.Height

	if width == 0 || height == 0 {
		return
	}

	if width == self.width && height == self.height {
		return
	}

	self.frame = resize.Resize(uint(width), uint(height), self.pImage, resize.Bilinear).(*image.RGBA)

	self.width = width
	self.height = height
}

func (self *Window) handleToplevelClose(_ xdg_shell.ToplevelCloseEvent) {
	self.exit = true
}

func _newWaylandWindow() (win *Window, err error) {
	win = &Window{}
	display, err := client.Connect("")
	if err != nil {
		return
	}
	win.display = display
	win.display.SetErrorHandler(win.handleDisplayError)

	registry, err := display.GetRegistry()
	if err != nil {
		return
	}
	win.registry = registry
	win.registry.SetGlobalHandler(win.handleRegistryGlobal)
	win.displayRoundTrip()
	win.displayRoundTrip()

	surface, err := win.compositor.CreateSurface()
	if err != nil {
		return
	}
	win.surface = surface

	xdgSurface, err := win.xdgWmBase.GetXdgSurface(win.surface)
	if err != nil {
		return
	}
	win.xdgSurface = xdgSurface
	win.xdgSurface.SetConfigureHandler(win.handleSurfaceConfigure)

	xdgTopLevel, err := xdgSurface.GetToplevel()
	if err != nil {
		return
	}
	win.xdgTopLevel = xdgTopLevel
	win.xdgTopLevel.SetConfigureHandler(win.handleToplevelConfigure)
	win.xdgTopLevel.SetCloseHandler(win.handleToplevelClose)

	// TODO: set title and appid
	if err = win.surface.Commit(); err != nil {
		return
	}

	theme, err := cursor.LoadTheme("default", 24, win.shm)
	if err != nil {
		return
	}
	win.cursorTheme = theme

	return
}

func newWaylandWindow() (*Window, error) {
	win, err := _newWaylandWindow()
	if err != nil {
		win.Close()
		win = nil
	}
	return win, err
}

func (self *Window) NextEvent() (event.Event, bool) {
	panic("TODO")
}

func (self *Window) Request(req event.Request) error {
	panic("TODO")
}

func NewWindow() (driver.Window, error) {
	culo
	win, err := newWaylandWindow()
	if err != nil {
		xwin, err2 := xdriver.NewWindow()
		if err2 != nil {
			return nil, err
		}
		return xwin, nil
	}
	return win, nil
}
