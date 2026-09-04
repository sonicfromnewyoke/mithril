package lightbringer

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/sonicfromnewyoke/mithril/pkg/mlog"
)

// Manager handles the lifecycle of a Lightbringer child process:
// config generation, spawning, log capture, health checking, and shutdown.
type Manager struct {
	binaryPath string
	configDir  string
	tomlCfg    LightbringerTOML
	grpcAddr   string
	logWriter  io.Writer

	cmd      *exec.Cmd
	done     chan struct{} // closed when the child process exits
	mu       sync.Mutex
	running  atomic.Bool
	stopping atomic.Bool // set by Stop to prevent MonitorAndRestart from restarting
}

// ManagerConfig holds the parameters needed to create a Manager.
type ManagerConfig struct {
	BinaryPath string
	ConfigDir  string
	GrpcAddr   string
	TOML       LightbringerTOML
	LogWriter  io.Writer // where captured stdout/stderr is written
}

// NewManager creates a new Lightbringer process manager.
func NewManager(cfg ManagerConfig) *Manager {
	return &Manager{
		binaryPath: cfg.BinaryPath,
		configDir:  cfg.ConfigDir,
		tomlCfg:    cfg.TOML,
		grpcAddr:   cfg.GrpcAddr,
		logWriter:  cfg.LogWriter,
	}
}

// WriteConfig generates Lightbringer.toml in the configured directory.
// Returns an error if required fields are missing.
func (m *Manager) WriteConfig() (string, error) {
	if err := m.tomlCfg.Validate(); err != nil {
		return "", fmt.Errorf("invalid lightbringer config: %w", err)
	}
	if err := os.MkdirAll(m.configDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create config directory %s: %w", m.configDir, err)
	}
	return m.tomlCfg.WriteConfigFile(m.configDir)
}

// Start spawns the Lightbringer process and begins capturing its output.
// On Linux, Pdeathsig ensures the kernel sends SIGTERM to Lightbringer if
// Mithril exits for any reason (including os.Exit, SIGKILL, panic).
func (m *Manager) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running.Load() {
		return fmt.Errorf("already running")
	}

	m.stopping.Store(false) // reset in case of previous Stop()

	// Resolve binary path to absolute before exec. Relative paths like
	// ./lightbringer are validated from cwd but cmd.Dir causes chdir(configDir)
	// before execve on Unix, so the binary would be looked up from configDir.
	// Making the path absolute here ensures consistent resolution.
	binaryAbs := m.binaryPath
	if !filepath.IsAbs(binaryAbs) {
		abs, err := filepath.Abs(binaryAbs)
		if err != nil {
			return fmt.Errorf("cannot resolve binary path %s: %w", m.binaryPath, err)
		}
		binaryAbs = abs
	}
	if _, err := os.Stat(binaryAbs); err != nil {
		return fmt.Errorf("binary not found at %s: %w", binaryAbs, err)
	}

	// Spawn in a dedicated goroutine with LockOSThread to keep Pdeathsig valid.
	// Pdeathsig fires when the *OS thread* that called fork() dies. Without
	// LockOSThread, Go may recycle the thread, triggering a premature death
	// signal while Mithril is still alive (Go issue #27505).
	type startResult struct {
		cmd    *exec.Cmd
		stdout io.ReadCloser
		stderr io.ReadCloser
		err    error
	}
	resultCh := make(chan startResult, 1)
	done := make(chan struct{})

	go func() {
		runtime.LockOSThread()
		// Do NOT defer UnlockOSThread here — we unlock only after Wait returns

		cmd := exec.Command(binaryAbs)
		cmd.Dir = m.configDir
		cmd.SysProcAttr = childSysProcAttr() // Pdeathsig on Linux, empty on other platforms

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			resultCh <- startResult{err: fmt.Errorf("stdout pipe: %w", err)}
			runtime.UnlockOSThread()
			return
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			resultCh <- startResult{err: fmt.Errorf("stderr pipe: %w", err)}
			runtime.UnlockOSThread()
			return
		}

		if err := cmd.Start(); err != nil {
			resultCh <- startResult{err: fmt.Errorf("failed to start: %w", err)}
			runtime.UnlockOSThread()
			return
		}

		resultCh <- startResult{cmd: cmd, stdout: stdout, stderr: stderr}

		// Block this goroutine (and its locked thread) until the child exits.
		// This keeps Pdeathsig valid for the entire lifetime of the child.
		waitErr := cmd.Wait()

		// Signal exit to the manager
		m.running.Store(false)
		if waitErr != nil {
			mlog.Log.Warnf("lightbringer: process exited with error: %v", waitErr)
		} else {
			mlog.Log.Infof("lightbringer: process exited cleanly")
		}
		close(done)

		runtime.UnlockOSThread()
	}()

	res := <-resultCh
	if res.err != nil {
		return res.err
	}

	m.cmd = res.cmd
	m.done = done
	m.running.Store(true)

	mlog.Log.Infof("lightbringer: started process (pid=%d, binary=%s)",
		res.cmd.Process.Pid, m.binaryPath)

	go m.captureOutput("stdout", res.stdout)
	go m.captureOutput("stderr", res.stderr)

	return nil
}

// WaitReady polls the gRPC endpoint until it accepts a TCP connection,
// or the timeout expires.
func (m *Manager) WaitReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	pollInterval := 500 * time.Millisecond

	for time.Now().Before(deadline) {
		if !m.running.Load() {
			return fmt.Errorf("process exited before becoming ready")
		}

		remaining := time.Until(deadline)
		dialTimeout := 2 * time.Second
		if remaining < dialTimeout {
			dialTimeout = remaining
		}

		conn, err := net.DialTimeout("tcp", m.grpcAddr, dialTimeout)
		if err == nil {
			_ = conn.Close()
			mlog.Log.Infof("lightbringer: gRPC endpoint ready at %s", m.grpcAddr)
			return nil
		}

		time.Sleep(pollInterval)
	}

	return fmt.Errorf("gRPC endpoint %s not ready after %s", m.grpcAddr, timeout)
}

// captureOutput reads from a pipe line-by-line and writes to the log writer.
func (m *Manager) captureOutput(name string, reader io.ReadCloser) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if m.logWriter != nil {
			fmt.Fprintf(m.logWriter, "%s\n", line)
		}
	}
	if err := scanner.Err(); err != nil {
		mlog.Log.Warnf("lightbringer: %s capture error: %v", name, err)
	}
}

// Stop sends SIGTERM to the Lightbringer process and waits for it to exit.
// If the process doesn't exit within the timeout, it sends SIGKILL.
func (m *Manager) Stop(timeout time.Duration) error {
	m.stopping.Store(true) // signal MonitorAndRestart to not restart
	m.mu.Lock()
	if !m.running.Load() {
		m.mu.Unlock()
		return nil
	}

	proc := m.cmd.Process
	done := m.done
	m.mu.Unlock()

	mlog.Log.Infof("lightbringer: sending SIGTERM to pid %d", proc.Pid)

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		mlog.Log.Warnf("lightbringer: SIGTERM failed: %v", err)
	}

	select {
	case <-done:
		mlog.Log.Infof("lightbringer: stopped cleanly")
		return nil
	case <-time.After(timeout):
		mlog.Log.Warnf("lightbringer: did not stop within %s, sending SIGKILL", timeout)
		if err := proc.Signal(syscall.SIGKILL); err != nil {
			mlog.Log.Warnf("lightbringer: SIGKILL failed: %v", err)
		}

		select {
		case <-done:
			return nil
		case <-time.After(5 * time.Second):
			return fmt.Errorf("process %d did not exit after SIGKILL", proc.Pid)
		}
	}
}

// IsRunning returns true if the Lightbringer process is currently running.
func (m *Manager) IsRunning() bool {
	return m.running.Load()
}

// Done returns a channel that is closed when the Lightbringer process exits.
// Returns nil if Start has not been called.
func (m *Manager) Done() <-chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.done
}

// Pid returns the PID of the running Lightbringer process, or 0 if not running.
func (m *Manager) Pid() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.running.Load() || m.cmd == nil || m.cmd.Process == nil {
		return 0
	}
	return m.cmd.Process.Pid
}

// MonitorAndRestart watches for unexpected Lightbringer exits and restarts
// with exponential backoff. Stops monitoring when stopCh is closed.
// maxRetries=0 means unlimited retries. Returns when stopped or max retries exceeded.
func (m *Manager) MonitorAndRestart(stopCh <-chan struct{}, maxRetries int) {
	backoff := 2 * time.Second
	maxBackoff := 60 * time.Second
	retries := 0

	for {
		done := m.Done()
		if done == nil {
			return
		}

		select {
		case <-stopCh:
			return
		case <-done:
		}

		// Process exited — wait for running flag to be cleared
		// (small window between done closing and running.Store(false))
		for i := 0; i < 10 && m.running.Load(); i++ {
			time.Sleep(10 * time.Millisecond)
		}

		// Check if this was a deliberate stop
		if m.stopping.Load() {
			return
		}
		select {
		case <-stopCh:
			return
		default:
		}

		retries++
		if maxRetries > 0 && retries > maxRetries {
			mlog.Log.Errorf("lightbringer: exceeded %d restart attempts, giving up", maxRetries)
			return
		}

		mlog.Log.Warnf("lightbringer: process exited unexpectedly, restarting in %s (attempt %d)", backoff, retries)

		select {
		case <-stopCh:
			return
		case <-time.After(backoff):
		}

		// Exponential backoff for next attempt
		backoff = backoff * 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}

		if _, err := m.WriteConfig(); err != nil {
			mlog.Log.Errorf("lightbringer: failed to write config for restart: %v", err)
			return
		}

		if err := m.Start(); err != nil {
			mlog.Log.Errorf("lightbringer: failed to restart: %v — giving up", err)
			return
		}
		mlog.Log.Infof("lightbringer: restarted successfully (attempt %d)", retries)
		continue // re-fetch new done channel at top of loop
	}
}
