package main

import (
	"flag"
	"fmt"
	"os"
	stdruntime "runtime"

	"github.com/ollykeran/sshush/internal/config"
	"github.com/ollykeran/sshush/internal/runtime"
	"github.com/ollykeran/sshush/internal/sshushd"
	"github.com/ollykeran/sshush/internal/style"
	"github.com/ollykeran/sshush/internal/version"
)

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.BoolVar(showVersion, "v", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Printf("sshushd %s (%s)\n", version.Version, stdruntime.Version())
		os.Exit(0)
	}
	configPath := runtime.ResolveDaemonConfigPath()

	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		style.NewOutput().Error("sshushd: load config: " + err.Error()).PrintErr()
		os.Exit(1)
	}
	if sshushd.CheckAlreadyRunning(cfg.SocketPath) {
		style.NewOutput().Error("sshushd: agent already running at " + cfg.SocketPath).PrintErr()
		os.Exit(1)
	}
	pidFilePath := runtime.PidFilePath()
	if err := sshushd.RunDaemonOnly(cfg.SocketPath, cfg.KeyPaths, pidFilePath); err != nil {
		style.NewOutput().Error("sshushd: " + err.Error()).PrintErr()
		os.Exit(1)
	}
}
