package utils

import (
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ollykeran/sshush/internal/openssh"
)

// ExpandHomeDirectory expands a leading "~" or "~/" for the current user only.
// Paths like "~otheruser/foo" are left unchanged.
func ExpandHomeDirectory(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return path
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	if path == "~" {
		return homeDir
	}
	if strings.HasPrefix(path, "~/") || (filepath.Separator == '\\' && strings.HasPrefix(path, `~\`)) {
		return filepath.Join(homeDir, path[2:])
	}
	return path
}

// ContractHomeDirectory replaces the current user's home directory prefix with ~.
func ContractHomeDirectory(path string) string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == homeDir {
		return "~"
	}
	if homeDir != "/" && strings.HasPrefix(path, homeDir+string(filepath.Separator)) {
		return "~" + string(filepath.Separator) + path[len(homeDir)+1:]
	}
	return path
}

// DisplayPath formats a path for user-visible output: it is made absolute when
// possible, then ContractHomeDirectory is applied so paths under $HOME use ~.
// A Windows path into a WSL distro, such as a key added from WSL, is shown as
// the Linux path it names (see WSLPath).
func DisplayPath(path string) string {
	if path == "" {
		return path
	}
	if _, linux, ok := WSLPath(path); ok {
		return ContractHomeDirectory(linux)
	}
	if filepath.Separator == '\\' && strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "//") {
		// A Linux path recorded by a WSL client: Abs would put a drive on it.
		return path
	}
	p := path
	if abs, err := filepath.Abs(path); err == nil {
		p = abs
	}
	return ContractHomeDirectory(p)
}

// WSLPath splits a Windows path into a WSL distro's files,
// \\wsl.localhost\<distro>\... or the older \\wsl$\<distro>\..., into the
// distro's name and the Linux path within it. ok is false for any other path.
// Either slash is accepted, as Windows accepts both.
func WSLPath(path string) (distro, linux string, ok bool) {
	p := strings.ReplaceAll(path, `\`, "/")
	var rest string
	for _, host := range []string{"//wsl.localhost/", "//wsl$/"} {
		if len(p) > len(host) && strings.EqualFold(p[:len(host)], host) {
			rest = p[len(host):]
			break
		}
	}
	if rest == "" {
		return "", "", false
	}
	distro, linux, _ = strings.Cut(rest, "/")
	if distro == "" {
		return "", "", false
	}
	return distro, "/" + linux, true
}

// SourcePath is how a key file's path is recorded in the vault as where the
// key came from. abs should be absolute. Inside WSL a Linux path is recorded
// as the Windows path into the distro (\\wsl.localhost\<distro>\...), and a
// path under /mnt/<drive> as the Windows path it is, so a Windows vault can
// tell which environment the key came from and open its folder. Elsewhere abs
// is returned unchanged.
func SourcePath(abs string) string {
	distro := os.Getenv("WSL_DISTRO_NAME")
	if filepath.Separator != '/' || distro == "" || !strings.HasPrefix(abs, "/") {
		return abs
	}
	if rest, ok := strings.CutPrefix(abs, "/mnt/"); ok && len(rest) >= 1 && isDriveLetter(rest[0]) &&
		(len(rest) == 1 || rest[1] == '/') {
		return strings.ToUpper(rest[:1]) + `:\` + strings.ReplaceAll(strings.TrimPrefix(rest[1:], "/"), "/", `\`)
	}
	return `\\wsl.localhost\` + distro + strings.ReplaceAll(abs, "/", `\`)
}

// LocalPath is a recorded source path (see SourcePath) as a path this process
// can open: inside WSL, a path into this distro or onto a Windows drive is
// made a Linux path again. Anything else is returned unchanged.
func LocalPath(recorded string) string {
	distro := os.Getenv("WSL_DISTRO_NAME")
	if filepath.Separator != '/' || distro == "" {
		return recorded
	}
	if d, linux, ok := WSLPath(recorded); ok {
		if strings.EqualFold(d, distro) {
			return linux
		}
		return recorded
	}
	if len(recorded) >= 3 && isDriveLetter(recorded[0]) && recorded[1] == ':' && (recorded[2] == '\\' || recorded[2] == '/') {
		p := "/mnt/" + strings.ToLower(recorded[:1])
		if rest := strings.Trim(strings.ReplaceAll(recorded[3:], `\`, "/"), "/"); rest != "" {
			p += "/" + rest
		}
		return p
	}
	return recorded
}

func isDriveLetter(c byte) bool {
	return ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z')
}

// Source describes where a key was added from, for display.
type Source struct {
	// Env is "Windows", "WSL (<distro>)", or "WSL" when the distro was not
	// recorded; empty when the path says nothing about where it lives.
	Env string
	// Display is the path in its environment's own form, with the home
	// directory shown as ~.
	Display string
	// Reveal is a Windows path to the file that Explorer can open, or empty
	// when there is none.
	Reveal string
	// Linux is the path within WSL when Env is "WSL" with no distro known.
	Linux string
}

// KeySource describes a key's recorded source path, as written by SourcePath
// or by an older client that recorded a bare path.
func KeySource(path string) Source {
	if distro, linux, ok := WSLPath(path); ok {
		return Source{Env: "WSL (" + distro + ")", Display: ContractLinuxHome(linux), Reveal: path}
	}
	if isWindowsAbs(path) {
		return Source{Env: "Windows", Display: contractWindowsHome(path), Reveal: path}
	}
	if strings.HasPrefix(path, "/") {
		if filepath.Separator == '\\' {
			// Recorded by a WSL client too old to say which distro.
			return Source{Env: "WSL", Display: ContractLinuxHome(path), Linux: path}
		}
		return Source{Display: ContractHomeDirectory(path)}
	}
	return Source{Display: path}
}

// ContractLinuxHome shows a path under /home/<user> or /root as under ~.
func ContractLinuxHome(linux string) string {
	if linux == "/root" {
		return "~"
	}
	if rest, ok := strings.CutPrefix(linux, "/root/"); ok {
		return "~/" + rest
	}
	if rest, ok := strings.CutPrefix(linux, "/home/"); ok {
		user, tail, more := strings.Cut(rest, "/")
		if user != "" {
			if !more {
				return "~"
			}
			return "~/" + tail
		}
	}
	return linux
}

// isWindowsAbs reports whether path is an absolute Windows path: a drive
// letter and slash, or a UNC path.
func isWindowsAbs(path string) bool {
	if len(path) >= 3 && isDriveLetter(path[0]) && path[1] == ':' && (path[2] == '\\' || path[2] == '/') {
		return true
	}
	return strings.HasPrefix(path, `\\`)
}

// contractWindowsHome is ContractHomeDirectory for a Windows path, matching the
// home directory without regard to case, as Windows does.
func contractWindowsHome(path string) string {
	if filepath.Separator != '\\' {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if strings.EqualFold(path, home) {
		return "~"
	}
	if len(path) > len(home) && strings.EqualFold(path[:len(home)], home) && (path[len(home)] == '\\' || path[len(home)] == '/') {
		return `~\` + path[len(home)+1:]
	}
	return path
}

// KeyPath is a discovered private key file path. If the path is a symlink,
// RealPath holds its resolved target (for exa-style "name -> target" display).
type KeyPath struct {
	Path      string
	RealPath  string
	IsSymlink bool
}

// UnreadableDirs returns the entries of dirs that cannot be listed as a
// directory: missing, not a directory, or without permission. DiscoverKeyPaths
// skips these silently; callers use this to say which ones were skipped.
func UnreadableDirs(dirs []string) []string {
	var out []string
	for _, d := range dirs {
		f, err := os.Open(d)
		if err != nil {
			out = append(out, d)
			continue
		}
		_, err = f.ReadDir(1)
		f.Close()
		if err != nil && err != io.EOF {
			out = append(out, d)
		}
	}
	return out
}

// KeySearchDirs is the list of directories DiscoverKeyPaths searches, in order:
// searchDirs, then ~/.ssh if ssh is true, then the current directory if cwd is
// true. It returns a new slice, so the caller's searchDirs (e.g. a config's
// search_dirs) is never written through.
func KeySearchDirs(searchDirs []string, cwd bool, ssh bool) []string {
	dirs := append([]string(nil), searchDirs...)
	if ssh {
		if home, err := os.UserHomeDir(); err == nil {
			dirs = append(dirs, filepath.Join(home, ".ssh"))
		}
	}
	if cwd {
		if wd, err := os.Getwd(); err == nil {
			dirs = append(dirs, wd)
		}
	}
	return dirs
}

// DiscoverKeyPaths finds valid private key files in searchDirs.
// If cwd is true, adds current directory. If ssh is true, adds ~/.ssh.
// If recursive is true, walks subdirectories. Directories that are missing or
// cannot be read are skipped.
//
// Entries are deduped by resolved (symlink-following) real path: a symlink
// and its target discovered separately collapse into a single KeyPath, shown
// in its symlink form (Path is the symlink, RealPath is the target).
func DiscoverKeyPaths(searchDirs []string, cwd bool, ssh bool, recursive bool) []KeyPath {
	seen := make(map[string]bool)
	seenReal := make(map[string]int) // real path -> index into paths
	var paths []KeyPath

	addPath := func(p string) {
		abs, err := filepath.Abs(p)
		if err != nil {
			abs = p
		}
		if seen[abs] {
			return
		}
		info, err := os.Lstat(abs)
		if err != nil {
			return
		}
		seen[abs] = true

		isSymlink := info.Mode()&os.ModeSymlink != 0
		real := abs
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			real = resolved
		}

		if idx, ok := seenReal[real]; ok {
			if isSymlink && !paths[idx].IsSymlink {
				paths[idx] = KeyPath{Path: abs, RealPath: real, IsSymlink: true}
			}
			return
		}
		seenReal[real] = len(paths)
		paths = append(paths, KeyPath{Path: abs, RealPath: real, IsSymlink: isSymlink})
	}

	// if configPath != "" {
	// 	cfg, err := config.LoadConfig(configPath)
	// 	if err == nil {
	// 		for _, p := range cfg.KeyPaths {
	// 			addPath(p)
	// 		}
	// 	}
	// }

	searchDirs = KeySearchDirs(searchDirs, cwd, ssh)

	tryAddKey := func(path string) {
		if seen[path] {
			return
		}
		data, err := os.ReadFile(path)
		if err != nil || len(data) == 0 {
			return
		}
		if _, err := openssh.ParsePrivateKeyBlob(data); err == nil {
			addPath(path)
		}
	}

	for _, dir := range searchDirs {
		if recursive {
			filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return nil
				}
				if d.IsDir() || strings.HasSuffix(d.Name(), ".pub") {
					return nil
				}
				tryAddKey(path)
				return nil
			})
		} else {
			entries, _ := os.ReadDir(dir)
			for _, e := range entries {
				if e.IsDir() || strings.HasSuffix(e.Name(), ".pub") {
					continue
				}
				tryAddKey(filepath.Join(dir, e.Name()))
			}
		}
	}

	return paths
}
