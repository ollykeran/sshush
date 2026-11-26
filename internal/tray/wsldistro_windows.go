package tray

import (
	"os"
	"strings"
	"sync"

	"golang.org/x/sys/windows/registry"
)

// distroProbe finds which WSL distro holds a key file recorded as a bare
// Linux path, by older clients that did not say. Looking can start a stopped
// distro and take seconds, so it runs off the window's thread and remembers
// what it found.
type distroProbe struct {
	mu      sync.Mutex
	found   map[string]string // Linux path -> the one distro holding it, or ""
	looking map[string]bool
}

// lookup returns the distro holding linux once known; known is false while it
// is still being looked for, and done is called when the search ends.
func (p *distroProbe) lookup(linux string, done func()) (distro string, known bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if d, ok := p.found[linux]; ok {
		return d, true
	}
	if p.looking[linux] {
		return "", false
	}
	if p.looking == nil {
		p.looking, p.found = map[string]bool{}, map[string]string{}
	}
	p.looking[linux] = true
	go func() {
		d := findDistro(linux)
		p.mu.Lock()
		p.found[linux] = d
		delete(p.looking, linux)
		p.mu.Unlock()
		done()
	}()
	return "", false
}

// findDistro is the one installed distro with a file at linux, or "" when
// none or several have one.
func findDistro(linux string) string {
	var hit string
	for _, d := range wslDistros() {
		if _, err := os.Stat(`\\wsl.localhost\` + d + strings.ReplaceAll(linux, "/", `\`)); err == nil {
			if hit != "" {
				return ""
			}
			hit = d
		}
	}
	return hit
}

// wslDistros lists the current user's installed WSL distros by name.
func wslDistros() []string {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Lxss`, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return nil
	}
	defer k.Close()
	subs, err := k.ReadSubKeyNames(-1)
	if err != nil {
		return nil
	}
	var out []string
	for _, s := range subs {
		sk, err := registry.OpenKey(k, s, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		name, _, err := sk.GetStringValue("DistributionName")
		sk.Close()
		if err == nil && name != "" {
			out = append(out, name)
		}
	}
	return out
}
