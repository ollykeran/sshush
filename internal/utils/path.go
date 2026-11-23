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
