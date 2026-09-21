package bedctl

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Proc root path, swappable in tests.
var procRoot = "/proc"

// ListenInode is the kernel socket inode of a listening TCP socket parsed
// from /proc/net/tcp{,6} tables.
type ListenInode struct {
	Port  uint16
	Inode string
}

// parseListenInodes extracts listening TCP socket inodes for the given port
// from the /proc-style directory root (covers IPv4 and IPv6). state 0A is
// TCP_LISTEN; the port in the table is big-endian hex, the address is
// irrelevant since any local interface binding counts.
func parseListenInodes(root string, port uint16) ([]ListenInode, error) {
	var out []ListenInode
	want := fmt.Sprintf("%04X", port)
	for _, table := range []string{"net/tcp", "net/tcp6"} {
		data, err := os.ReadFile(filepath.Join(root, table))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, line := range strings.Split(string(data), "\n") {
			fields := strings.Fields(line)
			// sl local_address state ... inode
			if len(fields) < 10 {
				continue
			}
			if fields[3] != "0A" {
				continue
			}
			localPort := strings.SplitN(fields[1], ":", 2)
			if len(localPort) != 2 || !strings.EqualFold(localPort[1], want) {
				continue
			}
			out = append(out, ListenInode{Port: port, Inode: fields[9]})
		}
	}
	return out, nil
}

// PIDsListeningOnPort finds processes with an open fd on a listening socket
// bound to port, by matching socket inodes against /proc/<pid>/fd entries.
// It returns an empty slice when nothing listens (or when /proc is absent,
// e.g. running on a non-Linux host — callers treat that as "unknown").
func PIDsListeningOnPort(port uint16) []int {
	return pidsListeningOnPort(procRoot, port)
}

func pidsListeningOnPort(root string, port uint16) []int {
	inodes, err := parseListenInodes(root, port)
	if err != nil || len(inodes) == 0 {
		return nil
	}
	want := map[string]bool{}
	for _, in := range inodes {
		want["socket:["+in.Inode+"]"] = true
	}
	var pids []int
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		fdDir := filepath.Join(root, e.Name(), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue // permission denied or process exited
		}
		for _, fd := range fds {
			link, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err == nil && want[link] {
				pids = append(pids, pid)
				break
			}
		}
	}
	return pids
}

// PIDsRunningBinary finds processes whose executable resolves to path (or
// whose cmdline starts with it when the exe link is unreadable, e.g. the
// binary was replaced on disk). Used to locate orphans that bypass the
// service manager.
func PIDsRunningBinary(path string) []int {
	return pidsRunningBinary(procRoot, path)
}

func pidsRunningBinary(root, path string) []int {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	var pids []int
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		if exe, err := os.Readlink(filepath.Join(root, e.Name(), "exe")); err == nil {
			if exe == abs || exe == path {
				pids = append(pids, pid)
			}
			continue
		}
		if cmdlineMatches(filepath.Join(root, e.Name(), "cmdline"), abs) {
			pids = append(pids, pid)
		}
	}
	return pids
}

func cmdlineMatches(cmdlineFile, path string) bool {
	data, err := os.ReadFile(cmdlineFile)
	if err != nil {
		return false
	}
	args := strings.Split(string(data), "\x00")
	return len(args) > 0 && (args[0] == path || filepath.Clean(args[0]) == path)
}

// ProcessAlive reports whether a PID currently exists and is not a zombie.
func ProcessAlive(pid int) bool {
	data, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "stat"))
	if err == nil {
		// Field 3 (state) sits after the comm field, which may contain
		// spaces wrapped in parentheses; take everything after the last ')'.
		s := string(data)
		i := strings.LastIndex(s, ")")
		if i < 0 || i+2 >= len(s) {
			return false
		}
		return s[i+2:i+3] != "Z"
	}
	// /proc unavailable (non-Linux host): fall back to signal 0 probing.
	if _, statErr := os.Stat(procRoot); statErr != nil {
		return syscall.Kill(pid, 0) == nil
	}
	return false
}

// PIDCmdLine returns the full command line of a PID, empty when unreadable.
func PIDCmdLine(pid int) string {
	return pidCmdLine(procRoot, pid)
}

func pidCmdLine(root string, pid int) string {
	data, err := os.ReadFile(filepath.Join(root, strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return ""
	}
	return strings.ReplaceAll(strings.Trim(string(data), "\x00"), "\x00", " ")
}
