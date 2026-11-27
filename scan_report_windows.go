package main

import (
	"fmt"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

var report struct {
	registered                   bool
	handle, edit, copy, save     uintptr
	scanIssues, conversionIssues []scanIssue
}

const (
	idCopyScanReport = 201
	idSaveReportCSV  = 202
)

func showScanReport(owner uintptr, scanIssues, conversionIssues []scanIssue) {
	showWindow := user32.NewProc("ShowWindow")
	activate := user32.NewProc("SetForegroundWindow")
	if report.handle != 0 {
		showWindow.Call(report.handle, 9) // SW_RESTORE
		activate.Call(report.handle)
		return
	}
	instance, _, _ := moduleHandle.Call(0)
	className := wide("2NCNScanReportWindow")
	if !report.registered {
		cursor, _, _ := user32.NewProc("LoadCursorW").Call(0, 32512)
		class := windowClass{
			Proc: syscall.NewCallback(scanReportProc), Instance: instance,
			Cursor: cursor, Background: 16, Name: className,
		}
		if registered, _, err := user32.NewProc("RegisterClassW").Call(uintptr(unsafe.Pointer(&class))); registered == 0 {
			message(owner, err.Error(), 0x10)
			return
		}
		report.registered = true
	}

	var content strings.Builder
	writeIssues := func(heading string, issues []scanIssue) {
		fmt.Fprintf(&content, "%s %d รายการ\r\n\r\n", heading, len(issues))
		for i, issue := range issues {
			fmt.Fprintf(&content, "%d. %s\r\n%s\r\n%s\r\n\r\n", i+1, issue.operation, issue.path, issue.reason)
		}
	}
	writeIssues("ปัญหาจากการสแกน", scanIssues)
	writeIssues("เพลงที่ข้ามในการแปลงครั้งล่าสุด", conversionIssues)
	title := wide("2NCN — รายงานปัญหา")
	const defaultPosition = 0x80000000
	handle, _, err := createWindow.Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(title)),
		0x00cf0000, defaultPosition, defaultPosition, 900, 570, owner, 0, instance, 0)
	if handle == 0 {
		message(owner, err.Error(), 0x10)
		return
	}

	report.handle = handle
	report.scanIssues, report.conversionIssues = scanIssues, conversionIssues
	report.edit = makeControl(handle, "EDIT", "", 0x00a10844, 200, instance)
	report.copy = makeControl(handle, "BUTTON", "คัดลอกทั้งหมด", 0x10000, idCopyScanReport, instance)
	report.save = makeControl(handle, "BUTTON", "บันทึก CSV", 0x10000, idSaveReportCSV, instance)
	if report.edit == 0 || report.copy == 0 || report.save == 0 {
		user32.NewProc("DestroyWindow").Call(handle)
		message(owner, "เปิดรายงานข้อผิดพลาดไม่ได้", 0x10)
		return
	}
	font, _, _ := sendMessage.Call(ui.search, 0x31, 0, 0)      // WM_GETFONT
	editFont, _, _ := gdi32.NewProc("GetStockObject").Call(17) // DEFAULT_GUI_FONT
	sendMessage.Call(report.edit, wmSetFont, editFont, 1)
	sendMessage.Call(report.copy, wmSetFont, font, 1)
	sendMessage.Call(report.save, wmSetFont, font, 1)
	sendMessage.Call(report.edit, 0xc5, 0x7ffffffe, 0) // EM_SETLIMITTEXT
	setWindowText.Call(report.edit, uintptr(unsafe.Pointer(wide(content.String()))))
	var rect [4]int32
	user32.NewProc("GetClientRect").Call(handle, uintptr(unsafe.Pointer(&rect[0])))
	layoutScanReport(int(rect[2]), int(rect[3]))
	showWindow.Call(handle, 5)
	activate.Call(handle)
	user32.NewProc("SetFocus").Call(report.edit)
}

func layoutScanReport(width, height int) {
	moveWindow.Call(report.copy, 12, 12, 145, 30, 1)
	moveWindow.Call(report.save, 165, 12, 145, 30, 1)
	moveWindow.Call(report.edit, 12, 52, uintptr(max(width-24, 0)), uintptr(max(height-64, 0)), 1)
}

func scanReportProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmSize:
		if report.edit != 0 {
			layoutScanReport(int(uint16(lParam)), int(uint16(lParam>>16)))
		}
	case wmCommand:
		switch uint16(wParam) {
		case idCopyScanReport:
			sendMessage.Call(report.edit, 0xb1, 0, ^uintptr(0)) // EM_SETSEL
			sendMessage.Call(report.edit, 0x301, 0, 0)          // WM_COPY
			return 0
		case idSaveReportCSV:
			saveReportFromDialog(hwnd)
			return 0
		}
	case wmDestroy:
		report.handle, report.edit, report.copy, report.save = 0, 0, 0, 0
		report.scanIssues, report.conversionIssues = nil, nil
		return 0
	}
	result, _, _ := defWindowProc.Call(hwnd, msg, wParam, lParam)
	return result
}

func saveReportFromDialog(owner uintptr) {
	const (
		explorer        = 0x80000
		pathMustExist   = 0x800
		noChangeDir     = 0x8
		overwritePrompt = 0x2
	)
	buffer := make([]uint16, 32768)
	copy(buffer, utf16.Encode([]rune("2NCN-report-"+time.Now().Format("20060102-150405")+".csv")))
	filter := utf16.Encode([]rune("CSV (*.csv)\x00*.csv\x00\x00"))
	dialog := openFileName{
		Owner: owner, Filter: &filter[0], FilterIndex: 1, File: &buffer[0],
		MaxFile: uint32(len(buffer)), Title: wide("บันทึกรายงานปัญหาเป็น CSV"),
		DefaultExtension: wide("csv"), Flags: explorer | pathMustExist | noChangeDir | overwritePrompt,
	}
	dialog.Size = uint32(unsafe.Sizeof(dialog))
	result, _, _ := comdlg32.NewProc("GetSaveFileNameW").Call(uintptr(unsafe.Pointer(&dialog)))
	runtime.KeepAlive(dialog)
	if result == 0 {
		code, _, _ := comdlg32.NewProc("CommDlgExtendedError").Call()
		if code != 0 {
			message(owner, fmt.Sprintf("เปิดหน้าต่างบันทึก CSV ไม่ได้ (0x%x)", code), 0x10)
		}
		return
	}

	path := syscall.UTF16ToString(buffer)
	if err := saveReportCSV(path, report.scanIssues, report.conversionIssues); err != nil {
		message(owner, fmt.Sprintf("บันทึกรายงาน CSV ไม่ได้\r\n%s", err), 0x10)
		return
	}
	message(owner, "บันทึกรายงาน CSV แล้ว\r\n"+path, 0x40)
}
