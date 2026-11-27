package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

const (
	wmDestroy           = 2
	wmSize              = 5
	wmNotify            = 0x4e
	wmCommand           = 0x111
	wmTimer             = 0x113
	wmSetFont           = 0x30
	idFiles             = 101
	idFolder            = 102
	idConvert           = 103
	idSearch            = 106
	idClear             = 107
	idSelected          = 108
	idVisible           = 109
	idStartCode         = 111
	idScanReport        = 114
	idStatusFilter      = 115
	wmContextMenu       = 0x7b
	lvmGetNextItem      = 0x100c
	lvmSubItemHitTest   = 0x1039
	lvmGetSelectedCount = 0x1032
	lvmSetItemState     = 0x102b
	lvisSelected        = 2
	lvmSetItemCount     = 0x102f
	lvmInsertColumnW    = 0x1061
	lvnGetDispInfoW     = ^uint32(176) // -177
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

type listItem struct {
	Mask             uint32
	Item, SubItem    int32
	State, StateMask uint32
	Text             *uint16
	TextMax, Image   int32
	Param            uintptr
}

type listNotification struct {
	Header        notifyHeader
	Item, SubItem int32
}

type displayInfo struct {
	Header notifyHeader
	listItem
}

type update struct {
	entry    song
	index    int
	result   string
	code     string
	showCode bool
	complete bool
	issues   []scanIssue
}

type catalogWindow struct {
	list, status, files, folder, convert          uintptr
	searchLabel, search, clear, selected, visible uintptr
	codeLabel, startCode, codeHint                uintptr
	shown                                         []int
	totalTasks                                    int
	scanIssues, conversionIssues                  []scanIssue
	statusFilter, statusLabel                     uintptr
	scanType, scanTypeLabel                       uintptr
	filter                                        songStatus
	sortColumn, sortOrder                         int
	reportButton                                  uintptr
	rows                                          []song
	updates                                       <-chan update
	converting                                    bool
	success, failures                             int
}

func makeControl(owner uintptr, class, caption string, style, id, instance uintptr) uintptr {
	handle, _, _ := createWindow.Call(0, uintptr(unsafe.Pointer(wide(class))),
		uintptr(unsafe.Pointer(wide(caption))), style|0x50000000, 0, 0, 0, 0, owner, id, instance, 0)
	return handle
}

func (w *catalogWindow) layout(width, height int) {
	width = max(width, 24)
	height = max(height, 201)
	moveWindow.Call(w.files, 12, 12, 130, 30, 1)
	moveWindow.Call(w.folder, 150, 12, 160, 30, 1)
	moveWindow.Call(w.selected, 318, 12, 160, 30, 1)
	moveWindow.Call(w.visible, 486, 12, 170, 30, 1)
	moveWindow.Call(w.convert, 664, 12, 170, 30, 1)
	moveWindow.Call(w.reportButton, 842, 12, 160, 30, 1)
	moveWindow.Call(w.searchLabel, 12, 55, 250, 26, 1)
	moveWindow.Call(w.search, 265, 50, uintptr(max(width-372, 0)), 30, 1)
	moveWindow.Call(w.clear, uintptr(max(width-97, 265)), 50, 85, 30, 1)
	moveWindow.Call(w.codeLabel, 12, 94, 250, 26, 1)
	moveWindow.Call(w.startCode, 265, 89, 140, 30, 1)
	moveWindow.Call(w.codeHint, 416, 94, uintptr(max(width-428, 0)), 26, 1)
	moveWindow.Call(w.statusLabel, 12, 132, 65, 26, 1)
	moveWindow.Call(w.statusFilter, 80, 127, 185, 180, 1)
	moveWindow.Call(w.scanTypeLabel, 285, 132, 170, 26, 1)
	moveWindow.Call(w.scanType, 455, 127, 155, 200, 1)
	moveWindow.Call(w.list, 12, 168, uintptr(width-24), uintptr(height-201), 1)
	moveWindow.Call(w.status, 12, uintptr(height-25), uintptr(width-24), 22, 1)
}

func (w *catalogWindow) statusText(text string) {
	setWindowText.Call(w.status, uintptr(unsafe.Pointer(wide(text))))
}

func (w *catalogWindow) setBusy(busy bool) {
	enableWindow.Call(w.files, enabled(!busy))
	enableWindow.Call(w.folder, enabled(!busy))
	enableWindow.Call(w.search, enabled(!busy))
	enableWindow.Call(w.clear, enabled(!busy))
	enableWindow.Call(w.startCode, enabled(!busy))
	enableWindow.Call(w.statusFilter, enabled(!busy))
	enableWindow.Call(w.scanType, enabled(!busy))
	enableWindow.Call(w.reportButton, enabled(!busy && len(w.scanIssues)+len(w.conversionIssues) > 0))
	w.updateActions(busy)
}

func enabled(value bool) uintptr {
	if value {
		return 1
	}
	return 0
}

func (w *catalogWindow) updateActions(busy bool) {
	selected, _, _ := sendMessage.Call(w.list, lvmGetSelectedCount, 0, 0)
	enableWindow.Call(w.selected, enabled(!busy && selected != 0))
	enableWindow.Call(w.visible, enabled(!busy && len(w.shown) != 0))
	enableWindow.Call(w.convert, enabled(!busy && len(w.rows) != 0))
}

func (w *catalogWindow) selection() []int {
	var rows []int
	index := ^uintptr(0)
	for {
		index, _, _ = sendMessage.Call(w.list, lvmGetNextItem, index, lvisSelected)
		if int32(index) < 0 || index >= uintptr(len(w.shown)) {
			return rows
		}
		rows = append(rows, w.shown[index])
	}
}

func (w *catalogWindow) setSelected(index uintptr, selected bool) {
	item := listItem{StateMask: lvisSelected}
	if selected {
		item.State = lvisSelected
	}
	sendMessage.Call(w.list, lvmSetItemState, index, uintptr(unsafe.Pointer(&item)))
	runtime.KeepAlive(item)
}

func (w *catalogWindow) applySearch() {
	var buffer [1025]uint16
	user32.NewProc("GetWindowTextW").Call(w.search, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	w.setSelected(^uintptr(0), false)
	w.shown = catalogView(w.rows, syscall.UTF16ToString(buffer[:]), w.filter, w.sortColumn, w.sortOrder)
	sendMessage.Call(w.list, lvmSetItemCount, uintptr(len(w.shown)), 0)
	if len(w.shown) > 0 {
		sendMessage.Call(w.list, 0x1013, 0, 0) // LVM_ENSUREVISIBLE
	}
	w.refreshList()
	w.updateActions(false)
	w.catalogStatus(false)
}

func (w *catalogWindow) refreshList() {
	user32.NewProc("InvalidateRect").Call(w.list, 0, 0)
}

func (w *catalogWindow) catalogStatus(scanning bool) {
	text := fmt.Sprintf("แสดง %d จาก %d เพลง", len(w.shown), len(w.rows))
	if scanning {
		text += "  |  กำลังอ่านรายการ…"
	} else if len(w.scanIssues) > 0 {
		text += fmt.Sprintf("  |  อ่านไฟล์หรือโฟลเดอร์ไม่ได้ %d รายการ", len(w.scanIssues))
	}
	w.statusText(text)
}

func (w *catalogWindow) load(paths []string, folder, extension string) {
	w.rows = nil
	w.shown = nil
	w.scanIssues, w.conversionIssues = nil, nil
	w.filter = allSongs
	sendMessage.Call(w.statusFilter, 0x14e, 0, 0) // CB_SETCURSEL
	if report.handle != 0 {
		user32.NewProc("DestroyWindow").Call(report.handle)
	}
	setWindowText.Call(w.search, uintptr(unsafe.Pointer(wide(""))))
	w.success, w.failures = 0, 0
	sendMessage.Call(w.list, lvmSetItemCount, 0, 0)
	w.setBusy(true)
	w.statusText("กำลังอ่านรายการเพลง…")
	updates := make(chan update, 256)
	w.updates = updates
	go func() {
		var issues []scanIssue
		visit := func(path string) {
			entry, err := songInfo(path)
			entry.status = uncheckedSong
			entry.state = "ยังไม่ตรวจ"
			if err != nil {
				entry.status = skippedSong
				entry.state = err.Error()
				issues = append(issues, scanIssue{operation: "อ่านไฟล์เพลง", path: path, reason: err.Error(), songCode: sourceSongCode(entry)})
			}
			updates <- update{entry: entry}
		}
		if folder != "" {
			scanSongs(folder, extension, visit, func(path string, err error) {
				issues = append(issues, scanIssue{operation: "สำรวจเส้นทาง", path: path, reason: err.Error()})
			})
		} else {
			for _, path := range paths {
				visit(path)
			}
		}
		updates <- update{complete: true, issues: issues}
	}()
}

func (w *catalogWindow) startConversion(owner uintptr, indices []int, flat bool) {
	if len(indices) == 0 || w.updates != nil {
		return
	}
	var codes []string
	if flat {
		var buffer [65]uint16
		user32.NewProc("GetWindowTextW").Call(w.startCode, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
		var err error
		codes, err = exportCodes(w.rows, indices, syscall.UTF16ToString(buffer[:]))
		if err != nil {
			message(owner, err.Error(), 0x30)
			user32.NewProc("SetFocus").Call(w.startCode)
			return
		}
	}
	folder, err := selectFolder(owner, fmt.Sprintf("เลือกโฟลเดอร์สำหรับเก็บ NCN (%d เพลง)", len(indices)))
	if err != nil {
		message(owner, err.Error(), 0x10)
		return
	}
	if folder == "" {
		return
	}
	w.setBusy(true)
	w.converting = true
	w.conversionIssues = nil
	if report.handle != 0 {
		user32.NewProc("DestroyWindow").Call(report.handle)
	}
	w.totalTasks = len(indices)
	w.success, w.failures = 0, 0
	w.statusText("กำลังแปลงเพลง…")
	type task struct {
		index      int
		path, code string
	}
	jobs := make([]task, 0, len(indices))
	for i, index := range indices {
		job := task{index: index, path: w.rows[index].path, code: sourceFileCode(w.rows[index].path)}
		if flat {
			job.code = codes[i]
		}
		jobs = append(jobs, job)
		w.rows[index].state = "รอแปลง"
	}
	w.refreshList()
	updates := make(chan update, 256)
	w.updates = updates
	go func() {
		for _, job := range jobs {
			result := ""
			var err error
			if flat {
				err = convertFlat(job.path, folder, job.code)
			} else {
				err = convert(job.path, folder)
			}
			if err != nil {
				result = err.Error()
			}
			updates <- update{index: job.index, result: result, code: job.code, showCode: flat}
		}
		updates <- update{complete: true}
	}()
}

func (w *catalogWindow) contextMenu(owner, coords uintptr) {
	if w.updates != nil || len(w.shown) == 0 {
		return
	}
	location := point{int32(int16(coords)), int32(int16(coords >> 16))}
	if uint32(coords) == ^uint32(0) {
		user32.NewProc("GetCursorPos").Call(uintptr(unsafe.Pointer(&location)))
	} else {
		hit := struct {
			Point                point
			Flags                uint32
			Item, SubItem, Group int32
		}{Point: location}
		user32.NewProc("ScreenToClient").Call(w.list, uintptr(unsafe.Pointer(&hit.Point)))
		index, _, _ := sendMessage.Call(w.list, lvmSubItemHitTest, 0, uintptr(unsafe.Pointer(&hit)))
		if int32(index) < 0 || hit.Flags&0x0e == 0 {
			return
		}
		state, _, _ := sendMessage.Call(w.list, 0x102c, index, lvisSelected) // LVM_GETITEMSTATE
		if state&lvisSelected == 0 {
			w.setSelected(^uintptr(0), false)
			w.setSelected(index, true)
		}
	}
	selected := w.selection()
	if len(selected) == 0 {
		return
	}
	menu, _, _ := user32.NewProc("CreatePopupMenu").Call()
	if menu == 0 {
		return
	}
	defer user32.NewProc("DestroyMenu").Call(menu)
	label := fmt.Sprintf("แปลงเฉพาะเพลงที่เลือก (%d เพลง)", len(selected))
	user32.NewProc("AppendMenuW").Call(menu, 0, idSelected, uintptr(unsafe.Pointer(wide(label))))
	command, _, _ := user32.NewProc("TrackPopupMenu").Call(menu, 0x182, uintptr(location.X), uintptr(location.Y), 0, owner, 0)
	if command == idSelected {
		w.startConversion(owner, selected, true)
	}
}

func (w *catalogWindow) poll() {
	if w.updates == nil {
		return
	}
	oldCount := len(w.rows)
	changed := false
	done := false
	for range 256 {
		select {
		case event := <-w.updates:
			if event.complete {
				done = true
				if !w.converting {
					w.scanIssues = event.issues
				}
				w.updates = nil
				break
			}
			if w.converting {
				changed = true
				row := &w.rows[event.index]
				row.state = event.result
				if event.result == "" {
					row.status = successfulSong
					row.state = "แปลงแล้ว"
					if event.showCode && event.code != "" {
						w.rows[event.index].state += " (" + event.code + ")"
					}
					w.success++
				} else {
					row.status = skippedSong
					w.conversionIssues = append(w.conversionIssues, scanIssue{
						operation: "ข้ามการแปลง", path: row.path, reason: event.result,
						songCode: sourceSongCode(*row), outputCode: event.code,
					})
					w.failures++
				}
			} else {
				w.shown = append(w.shown, len(w.rows))
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
	if done {
		w.applySearch()
	}
	if len(w.rows) != oldCount {
		sendMessage.Call(w.list, lvmSetItemCount, uintptr(len(w.shown)), 2)
	}
	if changed {
		w.refreshList()
	}
	if w.converting {
		if changed || done {
			w.statusText(fmt.Sprintf("แปลงสำเร็จ %d จาก %d เพลง   |   ข้าม %d",
				w.success, w.totalTasks, w.failures))
		}
	} else if len(w.rows) != oldCount || done {
		w.catalogStatus(!done)
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
				ui.load(paths, "", "")
			}
		case idFolder:
			extension := ""
			title := "เลือกโฟลเดอร์เพลงทุกชนิดที่รองรับ"
			selected, _, _ := sendMessage.Call(ui.scanType, 0x147, 0, 0) // CB_GETCURSEL
			if selected >= 1 && selected <= uintptr(len(songExtensions)) {
				extension = songExtensions[selected-1]
				title = "เลือกโฟลเดอร์เพลง " + strings.ToUpper(extension)
			}
			folder, err := selectFolder(hwnd, title)
			if err != nil {
				message(hwnd, err.Error(), 0x10)
			} else if folder != "" {
				ui.load(nil, folder, extension)
			}
		case idSearch:
			if uint16(wParam>>16) == 0x300 && ui.search != 0 { // EN_CHANGE
				ui.applySearch()
			}
		case idClear:
			setWindowText.Call(ui.search, uintptr(unsafe.Pointer(wide(""))))
			user32.NewProc("SetFocus").Call(ui.search)
		case idSelected:
			ui.startConversion(hwnd, ui.selection(), true)
		case idVisible:
			ui.startConversion(hwnd, ui.shown, true)
		case idStatusFilter:
			if uint16(wParam>>16) == 1 { // CBN_SELCHANGE
				selected, _, _ := sendMessage.Call(ui.statusFilter, 0x147, 0, 0) // CB_GETCURSEL
				if selected <= uintptr(uncheckedSong) {
					ui.filter = songStatus(selected)
					ui.applySearch()
				}
			}
		case idScanReport:
			showScanReport(hwnd, ui.scanIssues, ui.conversionIssues)
		case idConvert:
			ui.startConversion(hwnd, matchingSongs(ui.rows, ""), false)
		}
		return 0
	case wmContextMenu:
		if wParam == ui.list {
			ui.contextMenu(hwnd, lParam)
			return 0
		}
	case wmNotify:
		if lParam != 0 {
			header := (*notifyHeader)(unsafe.Pointer(lParam))
			if header.Window == ui.list {
				switch header.Code {
				case ^uint32(107): // LVN_COLUMNCLICK
					notification := (*listNotification)(unsafe.Pointer(lParam))
					column := int(notification.SubItem)
					if ui.updates == nil && column >= 0 && column < catalogColumnCount {
						if ui.sortColumn == column && ui.sortOrder == 1 {
							ui.sortOrder = -1
						} else {
							ui.sortOrder = 1
						}
						ui.sortColumn = column
						ui.applySearch()
					}
				case lvnGetDispInfoW:
					provideListText((*displayInfo)(unsafe.Pointer(lParam)))
					return 0
				case ^uint32(100), ^uint32(114): // LVN_ITEMCHANGED, LVN_ODSTATECHANGED
					ui.updateActions(ui.updates != nil)
				}
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
	if info.Mask&1 == 0 || index < 0 || index >= len(ui.shown) || column < 0 || column >= catalogColumnCount ||
		info.Text == nil || info.TextMax <= 0 {
		return
	}
	entry := ui.rows[ui.shown[index]]
	value, _ := syscall.UTF16FromString(strings.ReplaceAll(entry.columnText(column), "\x00", "�"))
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
	handle, _, err := createWindow.Call(0, uintptr(unsafe.Pointer(class.Name)), uintptr(unsafe.Pointer(wide("2NCN — รายการเพลง EMK / SMD / HNK / SIB / KAR"))),
		0x00cf0000, defaultPosition, defaultPosition, 1100, 680, 0, 0, instance, 0)
	if handle == 0 {
		message(0, err.Error(), 0x10)
		return
	}
	ui.files = makeControl(handle, "BUTTON", "เลือกไฟล์เพลง", 0x10000, idFiles, instance)
	ui.folder = makeControl(handle, "BUTTON", "สแกนโฟลเดอร์", 0x10000, idFolder, instance)
	ui.selected = makeControl(handle, "BUTTON", "แปลงที่เลือก", 0x10000, idSelected, instance)
	ui.visible = makeControl(handle, "BUTTON", "แปลงผลค้นหา", 0x10000, idVisible, instance)
	ui.convert = makeControl(handle, "BUTTON", "แปลงทั้งคลัง", 0x10000, idConvert, instance)
	ui.searchLabel = makeControl(handle, "STATIC", "ค้นหาชื่อเพลง / ศิลปิน / เวอร์ชัน / รูปแบบ", 0, 110, instance)
	ui.search = makeControl(handle, "EDIT", "", 0x810080, idSearch, instance)
	ui.clear = makeControl(handle, "BUTTON", "ล้างคำค้น", 0x10000, idClear, instance)
	ui.codeLabel = makeControl(handle, "STATIC", "รหัสเริ่มต้น (ผลค้นหา / เพลงที่เลือก)", 0, 112, instance)
	ui.startCode = makeControl(handle, "EDIT", "", 0x810080, idStartCode, instance)
	ui.codeHint = makeControl(handle, "STATIC", "เว้นว่างเพื่อใช้รหัสเดิม หรือใส่ 0001 / A0001 เพื่อรันเลข", 0, 113, instance)
	ui.list = makeControl(handle, "SysListView32", "", 0x81001, 104, instance)
	ui.statusLabel = makeControl(handle, "STATIC", "สถานะ", 0, 116, instance)
	ui.statusFilter = makeControl(handle, "COMBOBOX", "", 0x210003, idStatusFilter, instance)
	ui.scanTypeLabel = makeControl(handle, "STATIC", "ชนิดไฟล์สำหรับสแกน", 0, 117, instance)
	ui.scanType = makeControl(handle, "COMBOBOX", "", 0x210003, 118, instance)
	ui.reportButton = makeControl(handle, "BUTTON", "รายงานปัญหา", 0x10000, idScanReport, instance)
	ui.status = makeControl(handle, "STATIC", "", 0, 105, instance)
	widgets := []uintptr{ui.files, ui.folder, ui.selected, ui.visible, ui.convert,
		ui.searchLabel, ui.search, ui.clear, ui.codeLabel, ui.startCode, ui.codeHint, ui.list, ui.status, ui.reportButton, ui.statusLabel, ui.statusFilter, ui.scanTypeLabel, ui.scanType}
	for _, control := range widgets {
		if control == 0 {
			message(handle, "เปิดส่วนแสดงรายการเพลงไม่ได้", 0x10)
			return
		}
	}
	for _, label := range []string{"ทั้งหมด", "ผ่าน / แปลงแล้ว", "มีปัญหา", "ยังไม่ตรวจ"} {
		sendMessage.Call(ui.statusFilter, 0x143, 0, uintptr(unsafe.Pointer(wide(label)))) // CB_ADDSTRING
	}
	sendMessage.Call(ui.statusFilter, 0x14e, 0, 0)
	sendMessage.Call(ui.scanType, 0x143, 0, uintptr(unsafe.Pointer(wide("ทุกชนิด"))))
	for _, ext := range songExtensions {
		label := strings.ToUpper(strings.TrimPrefix(ext, "."))
		sendMessage.Call(ui.scanType, 0x143, 0, uintptr(unsafe.Pointer(wide(label))))
	}
	sendMessage.Call(ui.scanType, 0x14e, 0, 0)
	sendMessage.Call(ui.startCode, 0xc5, 64, 0)
	sendMessage.Call(ui.search, 0xc5, 1024, 0) // EM_SETLIMITTEXT

	font, _, _ := gdi32.NewProc("CreateFontW").Call(^uintptr(15), 0, 0, 0,
		400, 0, 0, 0, 1, 0, 0, 0, 0, uintptr(unsafe.Pointer(wide("Leelawadee UI"))))
	if font == 0 {
		font, _, _ = gdi32.NewProc("GetStockObject").Call(17)
	} else {
		defer gdi32.NewProc("DeleteObject").Call(font)
	}
	for _, control := range widgets {
		sendMessage.Call(control, wmSetFont, font, 1)
	}
	sendMessage.Call(ui.list, 0x2005, 1, 0)       // CCM_SETUNICODEFORMAT
	sendMessage.Call(ui.list, 0x1036, 0, 0x10021) // double buffer, full-row selection, grid lines
	labels := []string{"ชื่อเพลง", "ศิลปิน", "เวอร์ชัน / รูปแบบ", "ไฟล์ต้นทาง", "สถานะ"}
	widths := []int32{245, 175, 105, 350, 160}
	for i, label := range labels {
		column := listColumn{Mask: 6, Width: widths[i], Text: wide(label)}
		sendMessage.Call(ui.list, lvmInsertColumnW, uintptr(i), uintptr(unsafe.Pointer(&column)))
		runtime.KeepAlive(column)
	}
	ui.statusText("เลือกไฟล์ EMK / SMD / HNK / SIB / KAR หรือสแกนโฟลเดอร์เพื่อดูรายการเพลง")
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
			ui.load(nil, os.Args[1], "")
		} else {
			filtered := make([]string, 0, len(os.Args)-1)
			for _, path := range os.Args[1:] {
				if supportedSongPath(path) {
					filtered = append(filtered, path)
				}
			}
			if len(filtered) > 0 {
				ui.load(filtered, "", "")
			}
		}
	}
	getMessage := user32.NewProc("GetMessageW")
	translate := user32.NewProc("TranslateMessage")
	dispatch := user32.NewProc("DispatchMessageW")
	getKeyState := user32.NewProc("GetKeyState")
	setFocus := user32.NewProc("SetFocus")
	var msg winMessage
	for {
		result, _, _ := getMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(result) <= 0 {
			break
		}
		if msg.ID == 0x100 && ui.updates == nil { // WM_KEYDOWN
			modifier, _, _ := getKeyState.Call(0x11) // VK_CONTROL
			if modifier&0x8000 != 0 {
				switch msg.WParam {
				case 'F':
					setFocus.Call(ui.search)
					sendMessage.Call(ui.search, 0xb1, 0, ^uintptr(0)) // EM_SETSEL
					continue
				case 'A':
					if msg.Window == ui.search || msg.Window == ui.startCode || msg.Window == report.edit {
						sendMessage.Call(msg.Window, 0xb1, 0, ^uintptr(0))
						continue
					}
					if msg.Window == ui.list {
						ui.setSelected(^uintptr(0), true)
						ui.updateActions(false)
						continue
					}
				}
			}
		}
		translate.Call(uintptr(unsafe.Pointer(&msg)))
		dispatch.Call(uintptr(unsafe.Pointer(&msg)))
	}
}
