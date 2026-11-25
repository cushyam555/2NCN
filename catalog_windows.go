package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

const (
	wmDestroy        = 2
	wmSize           = 5
	wmNotify         = 0x4e
	wmCommand        = 0x111
	wmTimer          = 0x113
	wmSetFont        = 0x30
	idFiles          = 101
	idFolder         = 102
	idConvert        = 103
	lvmSetItemCount  = 0x102f
	lvmInsertColumnW = 0x1061
	lvmRedrawItems   = 0x1015
	lvnGetDispInfoW  = ^uint32(176) // -177
)

var (
	createWindow   = user32.NewProc("CreateWindowExW")
	sendMessage    = user32.NewProc("SendMessageW")
	setWindowText  = user32.NewProc("SetWindowTextW")
	enableWindow   = user32.NewProc("EnableWindow")
	moveWindow     = user32.NewProc("MoveWindow")
	defWindowProc  = user32.NewProc("DefWindowProcW")
	moduleHandle   = syscall.NewLazyDLL("kernel32.dll").NewProc("GetModuleHandleW")
	gdi32          = syscall.NewLazyDLL("gdi32.dll")
	commonControls = syscall.NewLazyDLL("comctl32.dll")
	ui             catalogWindow
)

type windowClass struct {
	Style                              uint32
	Proc                               uintptr
	ClassExtra, WindowExtra            int32
	Instance, Icon, Cursor, Background uintptr
	Menu, Name                         *uint16
}

type point struct{ X, Y int32 }

type winMessage struct {
	Window         uintptr
	ID             uint32
	WParam, LParam uintptr
	Time           uint32
	Point          point
	Private        uint32
}

type listColumn struct {
	Mask                                                               uint32
	Format, Width                                                      int32
	Text                                                               *uint16
	TextMax, SubItem, Image, Order, MinWidth, DefaultWidth, IdealWidth int32
}

type notifyHeader struct {
	Window, ID uintptr
	Code       uint32
}

type displayInfo struct {
	Header           notifyHeader
	Mask             uint32
	Item, SubItem    int32
	State, StateMask uint32
	Text             *uint16
	TextMax, Image   int32
	Param            uintptr
}

type update struct {
	entry    song
	index    int
	result   string
	complete bool
	problems int
}

type catalogWindow struct {
	list, status, files, folder, convert uintptr
	rows                                 []song
	updates                              <-chan update
	converting                           bool
	success, failures                    int
}

func makeControl(owner uintptr, class, caption string, style, id, instance uintptr) uintptr {
	handle, _, _ := createWindow.Call(0, uintptr(unsafe.Pointer(wide(class))),
		uintptr(unsafe.Pointer(wide(caption))), style|0x50000000, 0, 0, 0, 0, owner, id, instance, 0)
	return handle
}

func (w *catalogWindow) layout(width, height int) {
	width = max(width, 24)
	height = max(height, 85)
	moveWindow.Call(w.files, 12, 12, 130, 30, 1)
	moveWindow.Call(w.folder, 150, 12, 160, 30, 1)
	moveWindow.Call(w.convert, 318, 12, 160, 30, 1)
	moveWindow.Call(w.list, 12, 52, uintptr(width-24), uintptr(height-85), 1)
	moveWindow.Call(w.status, 12, uintptr(height-25), uintptr(width-24), 22, 1)
}

func (w *catalogWindow) statusText(text string) {
	setWindowText.Call(w.status, uintptr(unsafe.Pointer(wide(text))))
}

func (w *catalogWindow) setBusy(busy bool) {
	var sourceEnabled, convertEnabled uintptr
	if !busy {
		sourceEnabled = 1
		if len(w.rows) > 0 {
			convertEnabled = 1
		}
	}
	enableWindow.Call(w.files, sourceEnabled)
	enableWindow.Call(w.folder, sourceEnabled)
	enableWindow.Call(w.convert, convertEnabled)
}

func (w *catalogWindow) load(paths []string, folder string) {
	w.rows = nil
	w.success, w.failures = 0, 0
	sendMessage.Call(w.list, lvmSetItemCount, 0, 0)
	w.setBusy(true)
	w.statusText("กำลังอ่านรายการเพลง…")
	updates := make(chan update, 256)
	w.updates = updates
	go func() {
		problems := 0
		visit := func(path string) {
			entry, err := songInfo(path)
			if err != nil {
				entry.state = err.Error()
				problems++
			}
			updates <- update{entry: entry}
		}
		if folder != "" {
			scanSongs(folder, visit, func(_ string, _ error) { problems++ })
		} else {
			for _, path := range paths {
				visit(path)
			}
		}
		updates <- update{complete: true, problems: problems}
	}()
}

func (w *catalogWindow) convertAll(destination string) {
	w.setBusy(true)
	w.converting = true
	w.success, w.failures = 0, 0
	w.statusText("กำลังแปลงเพลง…")
	paths := make([]string, len(w.rows))
	for i, entry := range w.rows {
		paths[i] = entry.path
		w.rows[i].state = ""
	}
	sendMessage.Call(w.list, lvmRedrawItems, 0, uintptr(len(paths)-1))
	updates := make(chan update, 256)
	w.updates = updates
	go func() {
		for i, path := range paths {
			result := ""
			if err := convert(path, destination); err != nil {
				result = err.Error()
			}
			updates <- update{index: i, result: result}
		}
		updates <- update{complete: true}
	}()
}

func (w *catalogWindow) poll() {
	if w.updates == nil {
		return
	}
	oldCount := len(w.rows)
	first, last := -1, -1
	done, problems := false, 0
	for range 256 {
		select {
		case event := <-w.updates:
			if event.complete {
				done, problems = true, event.problems
				w.updates = nil
				break
			}
			if w.converting {
				if first < 0 {
					first = event.index
				}
				last = event.index
				w.rows[event.index].state = event.result
				if event.result == "" {
					w.rows[event.index].state = "แปลงแล้ว"
					w.success++
				} else {
					w.failures++
				}
			} else {
				w.rows = append(w.rows, event.entry)
			}
		default:
			goto refreshed
		}
		if done {
			break
		}
	}
refreshed:
	if len(w.rows) != oldCount {
		sendMessage.Call(w.list, lvmSetItemCount, uintptr(len(w.rows)), 2)
	}
	if first >= 0 {
		sendMessage.Call(w.list, lvmRedrawItems, uintptr(first), uintptr(last))
	}
	if w.converting {
		if first >= 0 || done {
			w.statusText(fmt.Sprintf("แปลงสำเร็จ %d จาก %d เพลง   |   มีข้อผิดพลาด %d",
				w.success, len(w.rows), w.failures))
		}
	} else if len(w.rows) != oldCount || done {
		text := fmt.Sprintf("พบ %d เพลง", len(w.rows))
		if !done {
			text += "  |  กำลังอ่านรายการ…"
		} else if problems > 0 {
			text += fmt.Sprintf("  |  อ่านไฟล์หรือโฟลเดอร์ไม่ได้ %d รายการ", problems)
		}
		w.statusText(text)
	}
	if done {
		w.converting = false
		w.setBusy(false)
	}
}

func windowProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmSize:
		if ui.list != 0 {
			ui.layout(int(uint16(lParam)), int(uint16(lParam>>16)))
		}
	case wmTimer:
		ui.poll()
	case wmCommand:
		if ui.updates != nil {
			return 0
		}
		switch uint16(wParam) {
		case idFiles:
			paths, err := selectFiles(hwnd)
			if err != nil {
				message(hwnd, err.Error(), 0x10)
			} else if len(paths) > 0 {
				ui.load(paths, "")
			}
		case idFolder:
			folder, err := selectFolder(hwnd, "เลือกโฟลเดอร์เพลง EMK")
			if err != nil {
				message(hwnd, err.Error(), 0x10)
			} else if folder != "" {
				ui.load(nil, folder)
			}
		case idConvert:
			if len(ui.rows) == 0 {
				return 0
			}
			folder, err := selectFolder(hwnd, "เลือกโฟลเดอร์สำหรับเก็บชุดไฟล์ NCN")
			if err != nil {
				message(hwnd, err.Error(), 0x10)
			} else if folder != "" {
				ui.convertAll(folder)
			}
		}
		return 0
	case wmNotify:
		if lParam != 0 {
			header := (*notifyHeader)(unsafe.Pointer(lParam))
			if header.Window == ui.list && header.Code == lvnGetDispInfoW {
				provideListText((*displayInfo)(unsafe.Pointer(lParam)))
				return 0
			}
		}
	case wmDestroy:
		user32.NewProc("PostQuitMessage").Call(0)
		return 0
	}
	result, _, _ := defWindowProc.Call(hwnd, msg, wParam, lParam)
	return result
}

func provideListText(info *displayInfo) {
	index, column := int(info.Item), int(info.SubItem)
	if info.Mask&1 == 0 || index < 0 || index >= len(ui.rows) || column < 0 || column >= 5 ||
		info.Text == nil || info.TextMax <= 0 {
		return
	}
	entry := ui.rows[index]
	columns := [5]string{entry.title, entry.artist, entry.version, entry.path, entry.state}
	value, _ := syscall.UTF16FromString(strings.ReplaceAll(columns[column], "\x00", "�"))
	buffer := unsafe.Slice(info.Text, int(info.TextMax))
	count := copy(buffer[:len(buffer)-1], value[:len(value)-1])
	buffer[count] = 0
}

func main() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	controls := struct{ Size, Classes uint32 }{8, 1}
	commonControls.NewProc("InitCommonControlsEx").Call(uintptr(unsafe.Pointer(&controls)))
	instance, _, _ := moduleHandle.Call(0)
	cursor, _, _ := user32.NewProc("LoadCursorW").Call(0, 32512)
	class := windowClass{
		Proc: syscall.NewCallback(windowProc), Instance: instance,
		Cursor: cursor, Background: 16, Name: wide("2NCNCatalogWindow"),
	}
	registered, _, err := user32.NewProc("RegisterClassW").Call(uintptr(unsafe.Pointer(&class)))
	if registered == 0 {
		message(0, err.Error(), 0x10)
		return
	}
	const defaultPosition = 0x80000000
	handle, _, err := createWindow.Call(0, uintptr(unsafe.Pointer(class.Name)), uintptr(unsafe.Pointer(wide("2NCN — รายการเพลง EMK"))),
		0x00cf0000, defaultPosition, defaultPosition, 1100, 680, 0, 0, instance, 0)
	if handle == 0 {
		message(0, err.Error(), 0x10)
		return
	}
	ui.files = makeControl(handle, "BUTTON", "เลือกไฟล์ EMK", 0x10000, idFiles, instance)
	ui.folder = makeControl(handle, "BUTTON", "สแกนโฟลเดอร์", 0x10000, idFolder, instance)
	ui.convert = makeControl(handle, "BUTTON", "แปลงทั้งหมดเป็น NCN", 0x10000, idConvert, instance)
	ui.list = makeControl(handle, "SysListView32", "", 0x81001, 104, instance)
	ui.status = makeControl(handle, "STATIC", "", 0, 105, instance)
	if ui.files == 0 || ui.folder == 0 || ui.convert == 0 || ui.list == 0 || ui.status == 0 {
		message(handle, "เปิดส่วนแสดงรายการเพลงไม่ได้", 0x10)
		return
	}
	font, _, _ := gdi32.NewProc("GetStockObject").Call(17)
	for _, control := range []uintptr{ui.files, ui.folder, ui.convert, ui.list, ui.status} {
		sendMessage.Call(control, wmSetFont, font, 1)
	}
	sendMessage.Call(ui.list, 0x2005, 1, 0)       // CCM_SETUNICODEFORMAT
	sendMessage.Call(ui.list, 0x1036, 0, 0x10021) // double buffer, full-row selection, grid lines
	labels := []string{"ชื่อเพลง", "ศิลปิน", "เวอร์ชัน EMK", "ไฟล์ต้นทาง", "สถานะ"}
	widths := []int32{245, 175, 105, 350, 160}
	for i, label := range labels {
		column := listColumn{Mask: 6, Width: widths[i], Text: wide(label)}
		sendMessage.Call(ui.list, lvmInsertColumnW, uintptr(i), uintptr(unsafe.Pointer(&column)))
		runtime.KeepAlive(column)
	}
	ui.statusText("เลือกไฟล์ EMK หรือสแกนโฟลเดอร์เพื่อดูรายการเพลง")
	ui.setBusy(false)
	var rect [4]int32
	user32.NewProc("GetClientRect").Call(handle, uintptr(unsafe.Pointer(&rect[0])))
	ui.layout(int(rect[2]), int(rect[3]))
	user32.NewProc("ShowWindow").Call(handle, 5)
	timer, _, _ := user32.NewProc("SetTimer").Call(handle, 1, 30, 0)
	if timer == 0 {
		message(handle, "เริ่มงานเบื้องหลังไม่ได้", 0x10)
		return
	}
	if len(os.Args) > 1 {
		if info, err := os.Stat(os.Args[1]); len(os.Args) == 2 && err == nil && info.IsDir() {
			ui.load(nil, os.Args[1])
		} else {
			filtered := make([]string, 0, len(os.Args)-1)
			for _, path := range os.Args[1:] {
				if strings.EqualFold(filepath.Ext(path), ".emk") {
					filtered = append(filtered, path)
				}
			}
			if len(filtered) > 0 {
				ui.load(filtered, "")
			}
		}
	}
	getMessage := user32.NewProc("GetMessageW")
	translate := user32.NewProc("TranslateMessage")
	dispatch := user32.NewProc("DispatchMessageW")
	var msg winMessage
	for {
		result, _, _ := getMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(result) <= 0 {
			break
		}
		translate.Call(uintptr(unsafe.Pointer(&msg)))
		dispatch.Call(uintptr(unsafe.Pointer(&msg)))
	}
}
