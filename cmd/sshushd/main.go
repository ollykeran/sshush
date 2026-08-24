package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/ollykeran/sshush/internal/config"
	"github.com/ollykeran/sshush/internal/readypipe"
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
		fmt.Println(version.Line("sshushd"))
		os.Exit(0)
	}

	ready := readypipe.FromEnv()

	configPath := runtime.ResolveDaemonConfigPath()
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		ready.Fail(err)
		style.NewOutput().Error("sshushd: load config: " + err.Error()).PrintErr()
		os.Exit(1)
	}

	if *serverMode {
		serverPidFilePath := runtime.ServerPidFilePath()
		if err := sshushd.RunServerOnly(cfg, serverPidFilePath, ready); err != nil {
			ready.Fail(err)
			style.NewOutput().Error("sshushd: " + err.Error()).PrintErr()
			os.Exit(1)
		}
		return
	}

	if sshushd.CheckAlreadyRunning(cfg.SocketPath) {
		err := fmt.Errorf("agent already running at %s", utils.DisplayPath(cfg.SocketPath))
		ready.Fail(err)
		style.NewOutput().Error("sshushd: " + err.Error()).PrintErr()
		os.Exit(1)
	}
	pidFilePath := runtime.PidFilePath()
	if err := sshushd.RunDaemonOnly(cfg, pidFilePath, ready); err != nil {
		ready.Fail(err)
		style.NewOutput().Error("sshushd: " + err.Error()).PrintErr()
		os.Exit(1)
	}
}
