package mutation

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"
)

// processTree is everything one test command starts, held in a job object. Processes that a process
// in a job starts join the job, so terminating the job ends them all: a go test killed alone would
// leave behind the test binaries, compilers and linkers it started, still running. The job is made
// to kill what it holds when its last handle closes, so the tree ends with hadome even when hadome
// is killed.
type processTree struct {
	cmd *exec.Cmd
	job syscall.Handle
}

var (
	kernel32                     = syscall.NewLazyDLL("kernel32.dll")
	procCreateJobObjectW         = kernel32.NewProc("CreateJobObjectW")
	procSetInformationJobObject  = kernel32.NewProc("SetInformationJobObject")
	procAssignProcessToJobObject = kernel32.NewProc("AssignProcessToJobObject")
	procTerminateJobObject       = kernel32.NewProc("TerminateJobObject")
	procThread32First            = kernel32.NewProc("Thread32First")
	procThread32Next             = kernel32.NewProc("Thread32Next")
	procOpenThread               = kernel32.NewProc("OpenThread")
	procResumeThread             = kernel32.NewProc("ResumeThread")
)

const (
	createSuspended                   = 0x00000004
	processSetQuota                   = 0x0100
	processTerminate                  = 0x0001
	threadSuspendResume               = 0x0002
	th32csSnapThread                  = 0x00000004
	jobObjectExtendedLimitInformation = 9
	jobObjectLimitKillOnJobClose      = 0x00002000
)

// jobLimits is JOBOBJECT_EXTENDED_LIMIT_INFORMATION. Its layout matches the
// C one on the 64-bit targets hadome is built for.
type jobLimits struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
	IoCounters              [6]uint64
	ProcessMemoryLimit      uintptr
	JobMemoryLimit          uintptr
	PeakProcessMemoryUsed   uintptr
	PeakJobMemoryUsed       uintptr
}

// threadEntry is THREADENTRY32.
type threadEntry struct {
	Size           uint32
	Usage          uint32
	ThreadID       uint32
	OwnerProcessID uint32
	BasePri        int32
	DeltaPri       int32
	Flags          uint32
}

// ownTree makes a job for the command and has the command start suspended, so that it cannot start
// a process before it is in the job.
func ownTree(cmd *exec.Cmd) (*processTree, error) {
	r, _, err := procCreateJobObjectW.Call(0, 0)
	if r == 0 {
		return nil, fmt.Errorf("creating a job object for the test command: %w", err)
	}
	job := syscall.Handle(r)
	limits := jobLimits{LimitFlags: jobObjectLimitKillOnJobClose}
	if r, _, err := procSetInformationJobObject.Call(uintptr(job), jobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits)); r == 0 {
		syscall.CloseHandle(job)
		return nil, fmt.Errorf("setting up the job object of the test command: %w", err)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createSuspended}
	cmd.Cancel = func() error {
		terminateJob(job)
		return nil
	}
	return &processTree{cmd: cmd, job: job}, nil
}

// started puts the suspended command in the job, then lets it run.
// A command that cannot be put in the job is killed and waited for, since nothing would end its tree.
func (t *processTree) started() error {
	if err := t.join(); err != nil {
		t.cmd.Process.Kill()
		t.cmd.Wait()
		return err
	}
	return nil
}

// join puts the started command in the job and resumes it.
func (t *processTree) join() error {
	pid := uint32(t.cmd.Process.Pid)
	h, err := syscall.OpenProcess(processSetQuota|processTerminate, false, pid)
	if err != nil {
		return fmt.Errorf("opening the test command to put it in a job: %w", err)
	}
	r, _, err := procAssignProcessToJobObject.Call(uintptr(t.job), uintptr(h))
	syscall.CloseHandle(h)
	if r == 0 {
		return fmt.Errorf("putting the test command in a job: %w", err)
	}
	return resume(pid)
}

// end terminates whatever is left of the tree after the command exited. A test command that leaves
// a process running (a server it started, a daemon of the build tool) would otherwise keep it
// running into the next mutant's run, and past hadome.
func (t *processTree) end() {
	terminateJob(t.job)
	syscall.CloseHandle(t.job)
}

func terminateJob(job syscall.Handle) {
	procTerminateJobObject.Call(uintptr(job), 1)
}

// resume resumes the threads of the process pid. A process started suspended has one.
func resume(pid uint32) error {
	snap, err := syscall.CreateToolhelp32Snapshot(th32csSnapThread, 0)
	if err != nil {
		return fmt.Errorf("listing the threads of the test command: %w", err)
	}
	defer syscall.CloseHandle(snap)
	e := threadEntry{Size: uint32(unsafe.Sizeof(threadEntry{}))}
	resumed := 0
	r, _, err := procThread32First.Call(uintptr(snap), uintptr(unsafe.Pointer(&e)))
	for ; r != 0; r, _, err = procThread32Next.Call(uintptr(snap), uintptr(unsafe.Pointer(&e))) {
		if e.OwnerProcessID != pid {
			continue
		}
		th, _, oerr := procOpenThread.Call(threadSuspendResume, 0, uintptr(e.ThreadID))
		if th == 0 {
			return fmt.Errorf("opening a thread of the test command: %w", oerr)
		}
		n, _, rerr := procResumeThread.Call(th)
		syscall.CloseHandle(syscall.Handle(th))
		if uint32(n) == 0xFFFFFFFF { // (DWORD)-1
			return fmt.Errorf("resuming the test command: %w", rerr)
		}
		resumed++
	}
	if !errors.Is(err, syscall.ERROR_NO_MORE_FILES) {
		return fmt.Errorf("listing the threads of the test command: %w", err)
	}
	if resumed == 0 {
		return errors.New("the test command has no thread to resume")
	}
	return nil
}
