package tray

import (
	"os"

	"github.com/ollykeran/sshush/internal/config"
	"github.com/ollykeran/sshush/internal/runtime"
)

// LoadConfig reads the config file the CLI would use with no --config flag. A
// missing file is not an error: the tray then targets the default socket.
func LoadConfig() (*config.Config, error) {
	path := runtime.ConfigPathDefault()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return &config.Config{}, nil
	}
	cfg, err := config.LoadConfig(path)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}
