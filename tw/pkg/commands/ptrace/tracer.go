//go:build linux
// +build linux

package ptrace

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"

	"github.com/armon/go-radix"
	"github.com/chainguard-dev/clog"
	"golang.org/x/sys/unix"
)

// SyscallState tracks the state of a syscall for a specific pid
type SyscallState struct {
	pid          int    // Process ID
	callNum      uint64 // Syscall number
	retVal       uint64 // Return value
	expectReturn bool   // Whether we're expecting a return from this syscall
	gotCallNum   bool   // Whether we've captured the syscall number
	gotRetVal    bool   // Whether we've captured the return value
	started      bool   // Whether the process has started
	exiting      bool   // Whether the process is exiting
	terminated   bool   // Whether the process has terminated
	parent       int    // Parent PID
	children     []int  // Child PIDs spawned by this process
	cmdline      string // Command line of the process (executable path)
	pathParam    string // Path parameter extracted from the syscall
}

// SyscallEvent represents a fully traced syscall event with call and return
type SyscallEvent struct {
	returned  bool   // Whether this includes the return value
	pid       int    // Process ID
	callNum   uint32 // Syscall number
	retVal    uint64 // Return value
	pathParam string // Path parameter extracted from the syscall
}

// Tracer provides syscall tracing functionality for processes
type Tracer struct {
	cmd           *exec.Cmd                  // Command to run and trace
	args          []string                   // Command arguments
	pgid          int                        // Process group ID
	syscallStats  map[uint32]uint64          // Statistics for each syscall
	fsActivity    map[string]*FSActivityInfo // File system activity
	eventCh       chan SyscallEvent          // Channel for syscall events
	done          chan struct{}              // Simple signal channel for completion
	result        chan TraceResult           // Channel for returning trace results
	ctx           context.Context            // Context for cancellation
	signalCh      chan os.Signal             // Channel for receiving signals
	pidSyscallMap map[int]*SyscallState      // Map of process IDs to syscall states
	handlers      map[int]SyscallHandler     // Syscall handlers by syscall number
	stdout        io.Writer                  // Standard output for the traced command
	stderr        io.Writer                  // Standard error for the traced command
	mu            sync.Mutex                 // Mutex for protecting shared data
}

// TraceResult represents the result of a trace operation
type TraceResult struct {
	ExitCode int
	Err      error
}

// FSActivityInfo tracks file system access information
type FSActivityInfo struct {
	OpsAll       uint64           // Total operations on this file/path
	OpsCheckFile uint64           // Operations that check file existence
	Pids         map[int]struct{} // Set of PIDs that accessed this path
	Syscalls     map[int]struct{} // Set of syscalls that accessed this path
}

// TracerOpts configures the tracer
type TracerOpts struct {
	Args     []string       // Command and arguments to trace
	Filter   []string       // Syscalls to filter (if empty, trace all)
	Stdout   io.Writer      // Standard output destination
	Stderr   io.Writer      // Standard error destination
	SignalCh chan os.Signal // Channel for receiving external signals
}

// SyscallHandler defines interface for handling different types of syscalls
type SyscallHandler interface {
	// SyscallNumber returns the syscall number this handler is responsible for
	SyscallNumber() int

	// SyscallName returns the human-readable name of the syscall
	SyscallName() string

	// SyscallType returns the type of syscall (check file, open file, exec, etc.)
	SyscallType() SyscallType

	// OnCall processes a syscall entry, extracting relevant information
	// from registers and updating the syscall state
	OnCall(pid int, regs syscall.PtraceRegs, state *SyscallState)

	// OnReturn processes a syscall exit, handling the return value
	// and updating the syscall state
	OnReturn(pid int, regs syscall.PtraceRegs, state *SyscallState)

	// OKReturnStatus determines if a return value indicates success
	// This varies by syscall type (e.g., open returns fd, stat returns 0 for success)
	OKReturnStatus(retVal uint64) bool
}

const ptOptions = syscall.PTRACE_O_TRACECLONE |
	syscall.PTRACE_O_TRACEFORK |
	syscall.PTRACE_O_TRACEVFORK |
	syscall.PTRACE_O_TRACEEXEC |
	syscall.PTRACE_O_TRACESYSGOOD |
	syscall.PTRACE_O_TRACEEXIT |
	unix.PTRACE_O_EXITKILL

// New creates a new tracer instance to trace the specified command
func New(command []string, opts TracerOpts) (*Tracer, error) {
	if len(command) == 0 {
		return nil, fmt.Errorf("no command specified")
	}

	t := &Tracer{
		args:          command,
		syscallStats:  make(map[uint32]uint64, 100),
		fsActivity:    make(map[string]*FSActivityInfo, 100),
		eventCh:       make(chan SyscallEvent, 2000),
		done:          make(chan struct{}),
		result:        make(chan TraceResult, 1),
		signalCh:      opts.SignalCh,
		pidSyscallMap: make(map[int]*SyscallState),
		handlers:      make(map[int]SyscallHandler),
		stdout:        os.Stdout,
		stderr:        os.Stderr,
	}

	if opts.Stdout != nil {
		t.stdout = opts.Stdout
	}

	if opts.Stderr != nil {
		t.stderr = opts.Stderr
	}

	// Register architecture-specific syscall handlers
	t.registerHandlers()

	return t, nil
}

// Start begins the tracing process
func (t *Tracer) Start(ctx context.Context) error {
	if t.result == nil {
		t.result = make(chan TraceResult, 1)
	}

	go t.processEvents(ctx)

	go func() {
		defer close(t.done)

		exitCode, err := t.trace(ctx)
		if err != nil {
			clog.ErrorContextf(ctx, "tracer failed: %v", err)
		}

		select {
		case t.result <- TraceResult{ExitCode: exitCode, Err: err}:
		case <-ctx.Done():
			clog.WarnContextf(ctx, "tracer result channel is closed, discarding result: %v", ctx.Err())
		default:
			// If channel is full, we'll just discard the result
			clog.WarnContext(ctx, "tracer result channel is full, discarding result")
		}
	}()

	// Start signal forwarding if we have a signal channel
	if t.signalCh != nil {
		go t.forwardSignals(ctx)
	}

	return nil
}

// forwardSignals forwards received signals to the traced process
func (t *Tracer) forwardSignals(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return

		case sig, ok := <-t.signalCh:
			if !ok {
				return
			}

			if sig == syscall.SIGCHLD {
				continue // Ignore SIGCHLD
			}

			// Convert to syscall.Signal if possible
			if ss, ok := sig.(syscall.Signal); ok {
				// Use syscall.Kill directly with the process group
				// because tracee's status is already captured by ptrace
				if err := syscall.Kill(t.cmd.Process.Pid, ss); err != nil {
					clog.ErrorContextf(ctx, "failed to forward signal %v: %v", ss, err)
				}
			}
		}
	}
}

// Wait waits for the tracing to complete and returns a report with statistics
func (t *Tracer) Wait() *TraceReport {
	// Wait for completion
	<-t.done

	// Get the result with exit code, safely handling an empty channel
	var result TraceResult
	select {
	case r, ok := <-t.result:
		if ok {
			result = r
		}
	default:
		// If result channel is empty, assume successful exit with no error
		result = TraceResult{ExitCode: 0, Err: nil}
	}

	// Create a deep copy of the statistics maps to prevent concurrent access
	t.mu.Lock()
	defer t.mu.Unlock()

	// Calculate total syscall count
	var totalSyscalls uint64
	for _, count := range t.syscallStats {
		totalSyscalls += count
	}

	// Copy syscall statistics to prevent concurrent modification
	syscallStats := make(map[uint32]uint64, len(t.syscallStats))
	for num, count := range t.syscallStats {
		syscallStats[num] = count
	}

	// Use a radix tree for efficient path hierarchy management
	tree := radix.New()
	for path, info := range t.fsActivity {
		tree.Insert(path, info)
	}

	// Copy and filter file activity information using the radix tree
	fsActivity := make(map[string]*FSActivityInfo, len(t.fsActivity))

	// Mark paths that are parents of other paths
	isParent := make(map[string]bool)

	// Walk through the tree to identify parent paths
	tree.Walk(func(path string, value interface{}) bool {
		// For each path, check if it's a prefix of other paths
		tree.WalkPrefix(path+"/", func(childPath string, childValue interface{}) bool {
			if childPath != path {
				isParent[path] = true
			}
			return false // Continue walking
		})
		return false // Continue walking
	})

	// Only include leaf paths (not parents) and create deep copies
	for path, info := range t.fsActivity {
		if !isParent[path] {
			// Create deep copy of the FSActivityInfo
			newInfo := &FSActivityInfo{
				OpsAll:       info.OpsAll,
				OpsCheckFile: info.OpsCheckFile,
				Pids:         make(map[int]struct{}, len(info.Pids)),
				Syscalls:     make(map[int]struct{}, len(info.Syscalls)),
			}

			// Copy process IDs that accessed this path
			for pid := range info.Pids {
				newInfo.Pids[pid] = struct{}{}
			}

			// Copy syscalls that accessed this path
			for syscallNum := range info.Syscalls {
				newInfo.Syscalls[syscallNum] = struct{}{}
			}

			fsActivity[path] = newInfo
		}
	}

	// Create and return the report
	report := &TraceReport{
		TotalSyscalls: totalSyscalls,
		ExitCode:      result.ExitCode,
		SyscallStats:  syscallStats,
		FSActivity:    fsActivity,
	}

	return report
}

// shouldBeIncluded checks if a path should be included in the final report
// It returns false if the path is a subpath of another path in the map
func shouldBeIncluded(path string, allPaths map[string]*FSActivityInfo) bool {
	// Always include the path itself
	if path == "" || path == "/" {
		return true
	}

	// Check if this path is a parent of any other path
	isParentPath := false
	for otherPath := range allPaths {
		if otherPath != path && strings.HasPrefix(otherPath, path+"/") {
			isParentPath = true
			break
		}
	}

	// Include if this is a leaf path (not a parent of any other path)
	return !isParentPath
}

// TraceReport contains the results of the tracing
type TraceReport struct {
	TotalSyscalls uint64                     // Total number of syscalls traced
	ExitCode      int                        // Exit code of the traced process
	SyscallStats  map[uint32]uint64          // Statistics for each syscall
	FSActivity    map[string]*FSActivityInfo // File system activity
}

// trace starts the ptrace process
func (t *Tracer) trace(ctx context.Context) (int, error) {
	if t.result == nil {
		t.result = make(chan TraceResult, 1)
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	cmd := exec.CommandContext(ctx, t.args[0], t.args[1:]...)
	cmd.Stdout = t.stdout
	cmd.Stderr = t.stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Ptrace: true,
	}

	if err := cmd.Start(); err != nil {
		return 1, fmt.Errorf("failed to start command: %w", err)
	}

	t.cmd = cmd

	// Wait for first stop - process stops after exec due to ptrace
	var ws syscall.WaitStatus
	_, err := syscall.Wait4(cmd.Process.Pid, &ws, 0, nil)
	if err != nil {
		return 1, fmt.Errorf("wait4 failed: %w", err)
	}

	// Get process group ID for signal handling
	t.pgid, err = syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		return 1, fmt.Errorf("failed to get process group: %w", err)
	}

	// Set comprehensive ptrace options
	err = syscall.PtraceSetOptions(cmd.Process.Pid, ptOptions)
	if err != nil {
		return 1, fmt.Errorf("failed to set ptrace options: %w", err)
	}

	// Initialize state for the main process
	t.pidSyscallMap[cmd.Process.Pid] = &SyscallState{
		pid: cmd.Process.Pid,
	}

	// Main tracing loop
	exitCode, err := t.traceLoop(ctx)
	return exitCode, err
}

// traceLoop is the main loop for tracking syscalls
func (t *Tracer) traceLoop(ctx context.Context) (int, error) {
	var callPid int
	callPid = t.cmd.Process.Pid

	callSig := 0
	waitFor := -1
	doSyscall := true

	// Create a cleanup function to ensure we detach from any remaining processes
	defer func() {
		// Clean up any remaining traced processes
		for pid := range t.pidSyscallMap {
			// Try to detach from the process
			_ = syscall.PtraceDetach(pid)
			delete(t.pidSyscallMap, pid)
		}
	}()

	for {
		// Check if context is done
		select {
		case <-ctx.Done():
			// Clean up and exit when context is cancelled
			for pid := range t.pidSyscallMap {
				_ = syscall.PtraceDetach(pid)
			}
			return 0, ctx.Err()
		default:
		}

		// Continue execution with syscall tracing
		if doSyscall {
			err := syscall.PtraceSyscall(callPid, callSig)
			if err != nil {
				// Handle process no longer existing
				if errno, ok := err.(syscall.Errno); ok && errno == syscall.ESRCH {
					delete(t.pidSyscallMap, callPid)
					doSyscall = false
					continue
				}
				// For other errors, try to clean up but continue with other processes
				doSyscall = false
				continue
			}
		}

		// Wait for syscall or other event with a check to ensure we're not blocking indefinitely
		var ws syscall.WaitStatus
		wpid, err := syscall.Wait4(waitFor, &ws, syscall.WALL, nil)
		if err != nil {
			if errno, ok := err.(syscall.Errno); ok && errno == syscall.ECHILD {
				// No more children - make sure we don't have any zombie pids in our map
				if len(t.pidSyscallMap) == 0 {
					return 0, nil
				}

				// Clean up any processes that might not have been properly waited for
				for pid := range t.pidSyscallMap {
					// Try to detach from any processes still in the map
					_ = syscall.PtraceDetach(pid)
					delete(t.pidSyscallMap, pid)
				}

				if len(t.pidSyscallMap) == 0 {
					return 0, nil
				}

				doSyscall = false
				continue
			}

			// Clean up before returning error
			for pid := range t.pidSyscallMap {
				_ = syscall.PtraceDetach(pid)
			}
			return 1, fmt.Errorf("wait4 failed: %w", err)
		}

		// Reset signal
		callSig = 0

		// Handle process termination
		if ws.Exited() || ws.Signaled() {
			delete(t.pidSyscallMap, wpid)

			// If main process terminated, we're done when all children are finished
			if wpid == t.cmd.Process.Pid {
				if len(t.pidSyscallMap) == 0 {
					return ws.ExitStatus(), nil
				}
			}

			doSyscall = false
			continue
		}

		// Handle syscall-stop or other stops
		if ws.Stopped() {
			stopSig := int(ws.StopSignal())

			// Check if this is a syscall stop
			if stopSig == int(syscall.SIGTRAP|0x80) {
				var cstate *SyscallState
				if state, ok := t.pidSyscallMap[wpid]; ok {
					cstate = state
				} else {
					// New process
					cstate = &SyscallState{pid: wpid}
					t.pidSyscallMap[wpid] = cstate
				}

				// Handle syscall entry or exit
				if !cstate.expectReturn {
					// Syscall entry
					var regs syscall.PtraceRegs
					if err := syscall.PtraceGetRegs(wpid, &regs); err == nil {
						callNum := getSyscallNumber(regs)
						cstate.callNum = callNum
						cstate.expectReturn = true
						cstate.gotCallNum = true

						// Process syscall entry if we have a handler
						if handler, ok := t.handlers[int(cstate.callNum)]; ok {
							handler.OnCall(wpid, regs, cstate)
						}
					} else if errno, ok := err.(syscall.Errno); ok && errno == syscall.ESRCH {
						// Process disappeared, clean it up
						delete(t.pidSyscallMap, wpid)
					}
				} else {
					// Syscall exit
					var regs syscall.PtraceRegs
					if err := syscall.PtraceGetRegs(wpid, &regs); err == nil {
						retVal := getReturnValue(regs)
						cstate.retVal = retVal
						cstate.expectReturn = false
						cstate.gotRetVal = true

						// Process syscall exit if we have a handler
						if handler, ok := t.handlers[int(cstate.callNum)]; ok {
							handler.OnReturn(wpid, regs, cstate)
						}

						// Send event
						if cstate.gotCallNum && cstate.gotRetVal {
							event := SyscallEvent{
								returned:  true,
								pid:       wpid,
								callNum:   uint32(cstate.callNum),
								retVal:    cstate.retVal,
								pathParam: cstate.pathParam,
							}

							select {
							case t.eventCh <- event:
							case <-ctx.Done():
								// Clean up and exit when context is cancelled
								for pid := range t.pidSyscallMap {
									_ = syscall.PtraceDetach(pid)
								}
								return 0, ctx.Err()
							default:
								// Channel buffer full, drop event
							}

							// Reset state
							cstate.gotCallNum = false
							cstate.gotRetVal = false
							cstate.pathParam = ""
						}
					} else if errno, ok := err.(syscall.Errno); ok && errno == syscall.ESRCH {
						// Process disappeared, clean it up
						delete(t.pidSyscallMap, wpid)
					}
				}
			} else if stopSig == int(syscall.SIGTRAP) {
				// Handle trace events (clone, fork, exec)
				cause := ws.TrapCause()

				switch cause {
				case syscall.PTRACE_EVENT_CLONE,
					syscall.PTRACE_EVENT_FORK,
					syscall.PTRACE_EVENT_VFORK:
					// New process created
					newPid, err := syscall.PtraceGetEventMsg(wpid)
					if err == nil {
						// Create new process state with parent relationship
						t.pidSyscallMap[int(newPid)] = &SyscallState{
							pid:     int(newPid),
							started: true,
							parent:  wpid, // Record parent process
						}

						// Update parent's children list
						if parentState, ok := t.pidSyscallMap[wpid]; ok {
							parentState.children = append(parentState.children, int(newPid))
						}

						// Try to read the command line for the new process
						// This might not be available immediately after fork/clone
						if cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", int(newPid))); err == nil {
							t.pidSyscallMap[int(newPid)].cmdline = string(cmdline)
						}
					}

				case syscall.PTRACE_EVENT_EXEC:
					// Process executed new program
					_, err := syscall.PtraceGetEventMsg(wpid)
					if err == nil {
						// Update command line after exec
						if cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", wpid)); err == nil {
							if state, ok := t.pidSyscallMap[wpid]; ok {
								state.cmdline = string(cmdline)
							}
						}

						// Record the exec event
						if state, ok := t.pidSyscallMap[wpid]; ok && state.pathParam != "" {
							// Generate an exec event if there's a path parameter
							event := SyscallEvent{
								returned:  false, // Exec doesn't return on success
								pid:       wpid,
								callNum:   uint32(state.callNum),
								pathParam: state.pathParam,
							}

							select {
							case t.eventCh <- event:
							case <-ctx.Done():
								// Clean up and exit when context is cancelled
								for pid := range t.pidSyscallMap {
									_ = syscall.PtraceDetach(pid)
								}
								return 0, ctx.Err()
							default:
								// Channel full, drop event
							}
						}
					}

				case syscall.PTRACE_EVENT_EXIT:
					// Process is about to exit
					if state, ok := t.pidSyscallMap[wpid]; ok {
						state.exiting = true

						// Special handling for main process exit
						if wpid == t.cmd.Process.Pid {
							// Don't delete the state yet, but mark it for special handling
							clog.InfoContextf(ctx, "main process %d is exiting", wpid)

							// If no children, we can detach and exit now
							if len(state.children) == 0 {
								// Return the actual exit status of the main process if available
								if ws.Exited() {
									return ws.ExitStatus(), nil
								}
								return 0, nil
							}
						}
					}
				}
			} else {
				// Forward signal to the process
				callSig = stopSig
			}
		}

		// Continue with next process
		doSyscall = true
		callPid = wpid
	}
}

// processEvents handles events from the trace process
func (t *Tracer) processEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			// Context was canceled, stop processing
			return

		case event, ok := <-t.eventCh:
			if !ok {
				// Channel closed, stop processing
				return
			}
			t.handleSyscallEvent(event)

		case <-t.done:
			// Tracing is complete
			return
		}
	}
}

// handleSyscallEvent processes a single syscall event
func (t *Tracer) handleSyscallEvent(event SyscallEvent) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Always record syscall statistics
	t.syscallStats[event.callNum]++

	// Skip events without path parameters
	if event.pathParam == "" {
		return
	}

	// Apply path filtering early - use global function instead of method
	if shouldSkipPath(event.pathParam) {
		return
	}

	// Skip events that don't have a handler
	handler, ok := t.handlers[int(event.callNum)]
	if !ok {
		return // No handler for this syscall
	}

	// Process according to syscall type
	switch handler.SyscallType() {
	case CheckFileType:
		// For check operations, always record
		t.recordFileActivity(event, handler)

	case OpenFileType:
		// For open operations, only record successful ones
		if !event.returned || handler.OKReturnStatus(event.retVal) {
			t.recordFileActivity(event, handler)
		}

	case ExecType:
		// For exec operations, always record
		t.recordFileActivity(event, handler)
	}
}

// recordFileActivity records file access activity
func (t *Tracer) recordFileActivity(event SyscallEvent, handler SyscallHandler) {
	if fsa, ok := t.fsActivity[event.pathParam]; ok {
		// Update existing record
		fsa.OpsAll++
		if handler.SyscallType() == CheckFileType {
			fsa.OpsCheckFile++
		}

		// Record PID and syscall
		if fsa.Pids == nil {
			fsa.Pids = make(map[int]struct{})
		}
		fsa.Pids[event.pid] = struct{}{}

		if fsa.Syscalls == nil {
			fsa.Syscalls = make(map[int]struct{})
		}
		fsa.Syscalls[int(event.callNum)] = struct{}{}
	} else {
		// Create new record
		fsa := &FSActivityInfo{
			OpsAll:       1,
			OpsCheckFile: 0,
			Pids:         map[int]struct{}{},
			Syscalls:     map[int]struct{}{},
		}

		if handler.SyscallType() == CheckFileType {
			fsa.OpsCheckFile = 1
		}

		fsa.Pids[event.pid] = struct{}{}
		fsa.Syscalls[int(event.callNum)] = struct{}{}

		t.fsActivity[event.pathParam] = fsa
	}
}
