package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func main() {
	profile := flag.String("profile", "coverage.out", "Go coverage profile to summarize")
	module := flag.String("module", "github.com/ollykeran/sshush", "module path used as the coverage profile root")
	flag.Parse()

	f, err := os.Open(*profile)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()

	stats := make(map[string][2]int)
	total, covered := 0, 0

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		file := fields[0]
		if i := strings.LastIndex(file, ":"); i >= 0 {
			file = file[:i]
		}
		numStmt, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		count, err := strconv.Atoi(fields[2])
		if err != nil {
			continue
		}

		pkg := packageOf(file, *module)
		s := stats[pkg]
		s[0] += numStmt
		if count > 0 {
			s[1] += numStmt
		}
		stats[pkg] = s

		total += numStmt
		if count > 0 {
			covered += numStmt
		}
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	pkgs := make([]string, 0, len(stats))
	for pkg := range stats {
		pkgs = append(pkgs, pkg)
	}
	sort.Strings(pkgs)

	fmt.Println("| Package | Statements | Coverage |")
	fmt.Println("|---|---|---|")
	for _, pkg := range pkgs {
		s := stats[pkg]
		fmt.Printf("| `%s` | %d | %.1f%% |\n", pkg, s[0], pct(s[1], s[0]))
	}
	fmt.Printf("| **Total** | **%d** | **%.1f%%** |\n", total, pct(covered, total))
}

func packageOf(file, module string) string {
	if strings.HasPrefix(file, module+"/") {
		file = strings.TrimPrefix(file, module+"/")
	}
	dir := filepath.Dir(file)
	if dir == "." {
		return "root"
	}
	return dir
}

func pct(cov, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(cov) / float64(total) * 100
}
