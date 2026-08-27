package utils

import (
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
	if strings.HasPrefix(path, "~/") {
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
func DisplayPath(path string) string {
	if path == "" {
		return path
	}
	p := path
	if abs, err := filepath.Abs(path); err == nil {
		p = abs
	}
	return ContractHomeDirectory(p)
}

// KeyPath is a discovered private key file path. If the path is a symlink,
// RealPath holds its resolved target (for exa-style "name -> target" display).
type KeyPath struct {
	Path      string
	RealPath  string
	IsSymlink bool
}

// DiscoverKeyPaths finds valid private key files in searchDirs.
// If cwd is true, adds current directory. If ssh is true, adds ~/.ssh.
// If recursive is true, walks subdirectories.
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

	if ssh {
		if home, err := os.UserHomeDir(); err == nil {
			searchDirs = append(searchDirs, filepath.Join(home, ".ssh"))
		}
	}
	if cwd {
		if cwd, err := os.Getwd(); err == nil {
			searchDirs = append(searchDirs, cwd)
		}
	}

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
