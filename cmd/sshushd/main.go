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
	"github.com/ollykeran/sshush/internal/utils"
	"github.com/ollykeran/sshush/internal/version"
)

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.BoolVar(showVersion, "v", false, "print version and exit")
	serverMode := flag.Bool("server", false, "run TCP SSH server daemon only")
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

	if *serverMode {
		serverPidFilePath := runtime.ServerPidFilePath()
		if err := sshushd.RunServerOnly(cfg, serverPidFilePath); err != nil {
			style.NewOutput().Error("sshushd: " + err.Error()).PrintErr()
			os.Exit(1)
		}
		return
	}

	if sshushd.CheckAlreadyRunning(cfg.SocketPath) {
		style.NewOutput().Error("sshushd: agent already running at " + utils.DisplayPath(cfg.SocketPath)).PrintErr()
		os.Exit(1)
	}
	pidFilePath := runtime.PidFilePath()
	if err := sshushd.RunDaemonOnly(cfg, pidFilePath); err != nil {
		style.NewOutput().Error("sshushd: " + err.Error()).PrintErr()
		os.Exit(1)
	}
}
