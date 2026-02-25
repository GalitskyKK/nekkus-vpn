//go:build windows

package vpn

import (
	"syscall"
	"unsafe"
)

var (
	kernel32     = syscall.NewLazyDLL("kernel32.dll")
	createJobObj = kernel32.NewProc("CreateJobObjectW")
	setInfoJob   = kernel32.NewProc("SetInformationJobObject")
	assignProc   = kernel32.NewProc("AssignProcessToJobObject")
	getCurrProc  = kernel32.NewProc("GetCurrentProcess")
)

const (
	jobObjectExtendedLimitInformation = 9
	jobObjectLimitKillOnJobClose      = 0x2000
)

// jobObjectExtendedLimitInfoStruct for SetInformationJobObject
type jobObjectExtendedLimitInfoStruct struct {
	BasicLimitInformation struct {
		PerProcessUserTimeLimit int64
		PerJobUserTimeLimit     int64
		LimitFlags              uint32
		MinWorkingSetSize       uintptr
		MaxWorkingSetSize       uintptr
		ActiveProcessLimit      uint32
		Affinity                uintptr
		PriorityClass           uint32
		SchedulingClass         uint32
	}
	IoInfo                [0]byte
	ProcessMemoryLimit     uintptr
	JobMemoryLimit        uintptr
	ProcessMemoryLimitVal  uintptr
	JobMemoryLimitVal      uintptr
}

// EnsureChildProcessesKillOnExit привязывает текущий процесс к Job Object с флагом
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE, чтобы при выходе процесса все дочерние (sing-box)
// автоматически завершались. Вызывать из main() при старте на Windows.
func EnsureChildProcessesKillOnExit() {
	attr, err := syscall.UTF16PtrFromString("")
	if err != nil {
		return
	}
	job, _, err := createJobObj.Call(uintptr(unsafe.Pointer(&syscall.SecurityAttributes{})), uintptr(unsafe.Pointer(attr)))
	if job == 0 {
		return
	}
	jobHandle := syscall.Handle(job)
	keepJobOpen := false
	defer func() {
		if !keepJobOpen && jobHandle != 0 {
			syscall.CloseHandle(jobHandle)
		}
	}()

	var info jobObjectExtendedLimitInfoStruct
	info.BasicLimitInformation.LimitFlags = jobObjectLimitKillOnJobClose
	r, _, _ := setInfoJob.Call(job, jobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uintptr(unsafe.Sizeof(info)))
	if r == 0 {
		return
	}

	curProc, _, _ := getCurrProc.Call()
	if curProc == 0 {
		return
	}
	r, _, _ = assignProc.Call(job, curProc)
	if r == 0 {
		return
	}
	keepJobOpen = true // не закрывать: при выходе процесса job уничтожится, sing-box завершится
}
