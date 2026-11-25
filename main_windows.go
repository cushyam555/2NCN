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

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	comdlg32 = syscall.NewLazyDLL("comdlg32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	ole32    = syscall.NewLazyDLL("ole32.dll")
)

type openFileName struct {
	Size                         uint32
	Owner, Instance              uintptr
	Filter, CustomFilter         *uint16
	MaxCustomFilter, FilterIndex uint32
	File                         *uint16
	MaxFile                      uint32
	FileTitle                    *uint16
	MaxFileTitle                 uint32
	InitialDir, Title            *uint16
	Flags                        uint32
	FileOffset, FileExtension    uint16
	DefaultExtension             *uint16
	CustomData, Hook             uintptr
	TemplateName                 *uint16
	Reserved                     uintptr
	ReservedSize, ExtendedFlags  uint32
}

type browseInfo struct {
	Owner, Root        uintptr
	DisplayName, Title *uint16
	Flags              uint32
	Callback, Param    uintptr
	Image              int32
}

func wide(s string) *uint16 {
	s = strings.ReplaceAll(s, "\x00", "�")
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func message(body string, icon uintptr) {
	user32.NewProc("MessageBoxW").Call(0,
		uintptr(unsafe.Pointer(wide(body))),
		uintptr(unsafe.Pointer(wide("2NCN — EMK → NCN"))), icon)
}

func selectFiles() ([]string, error) {
	const (
		explorer         = 0x80000
		allowMultiSelect = 0x200
		fileMustExist    = 0x1000
		noChangeDir      = 0x8
	)
	buffer := make([]uint16, 65536)
	filter := []uint16{'E', 'M', 'K', 0, '*', '.', 'e', 'm', 'k', 0, 0}
	dialog := openFileName{
		Filter: &filter[0], FilterIndex: 1, File: &buffer[0],
		MaxFile: uint32(len(buffer)), Title: wide("เลือกไฟล์ EMK"),
		Flags: explorer | allowMultiSelect | fileMustExist | noChangeDir,
	}
	dialog.Size = uint32(unsafe.Sizeof(dialog))
	result, _, _ := comdlg32.NewProc("GetOpenFileNameW").Call(uintptr(unsafe.Pointer(&dialog)))
	runtime.KeepAlive(dialog)
	if result == 0 {
		code, _, _ := comdlg32.NewProc("CommDlgExtendedError").Call()
		if code != 0 {
			return nil, fmt.Errorf("เปิดหน้าต่างเลือกไฟล์ไม่ได้ (0x%x)", code)
		}
		return nil, nil
	}
	var entries []string
	start := 0
	for end, char := range buffer {
		if char != 0 {
			continue
		}
		if start == end {
			break
		}
		entries = append(entries, syscall.UTF16ToString(buffer[start:end]))
		start = end + 1
	}
	if len(entries) <= 1 {
		return entries, nil
	}
	paths := entries[1:]
	for i := range paths {
		paths[i] = filepath.Join(entries[0], paths[i])
	}
	return paths, nil
}

func selectDestination() (string, error) {
	const (
		apartmentThreaded = 2
		returnFSDirs      = 1
		newDialogStyle    = 0x40
	)
	initialized, _, _ := ole32.NewProc("CoInitializeEx").Call(0, apartmentThreaded)
	if int32(initialized) < 0 {
		return "", fmt.Errorf("เปิดหน้าต่างเลือกโฟลเดอร์ไม่ได้ (0x%x)", uint32(initialized))
	}
	defer ole32.NewProc("CoUninitialize").Call()

	var name [260]uint16
	options := browseInfo{
		DisplayName: &name[0], Title: wide("เลือกโฟลเดอร์สำหรับเก็บชุดไฟล์ NCN"),
		Flags: returnFSDirs | newDialogStyle,
	}
	folder, _, _ := shell32.NewProc("SHBrowseForFolderW").Call(uintptr(unsafe.Pointer(&options)))
	runtime.KeepAlive(options)
	if folder == 0 {
		return "", nil
	}
	defer ole32.NewProc("CoTaskMemFree").Call(folder)
	var path [260]uint16
	result, _, _ := shell32.NewProc("SHGetPathFromIDListW").Call(folder, uintptr(unsafe.Pointer(&path[0])))
	if result == 0 {
		return "", fmt.Errorf("ไม่สามารถอ่านที่อยู่โฟลเดอร์ปลายทาง")
	}
	return syscall.UTF16ToString(path[:]), nil
}

func main() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	paths := os.Args[1:]
	if len(paths) == 0 {
		var err error
		paths, err = selectFiles()
		if err != nil {
			message(err.Error(), 0x10)
			return
		}
	}
	if len(paths) == 0 {
		return
	}
	destination, err := selectDestination()
	if err != nil {
		message(err.Error(), 0x10)
		return
	}
	if destination == "" {
		return
	}

	var failures []string
	for _, path := range paths {
		if err := convert(path, destination); err != nil {
			failures = append(failures, fmt.Sprintf("%s — %v", filepath.Base(path), err))
		}
	}
	summary := fmt.Sprintf("แปลงสำเร็จ %d จาก %d เพลง\n%s", len(paths)-len(failures), len(paths), destination)
	if len(failures) == 0 {
		message(summary, 0x40)
		return
	}
	if len(failures) > 10 {
		summary += "\n\n" + strings.Join(failures[:10], "\n") + fmt.Sprintf("\nและอีก %d ไฟล์", len(failures)-10)
	} else {
		summary += "\n\n" + strings.Join(failures, "\n")
	}
	message(summary, 0x30)
}
