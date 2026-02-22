package utils

import (
	"os"
	"strings"
)

func ExpandHomeDirectory(path string) (string, error) {
	if strings.Contains(path, "~") {
		home_dir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return strings.ReplaceAll(path, "~", home_dir), nil
	}	
	return path, nil
}

// func ExpandRelativePath(path string) (string, error) {
// 	if strings.HasPrefix(path, "./") {
// 		cwd, err := os.Getwd()
// 		if err != nil {
// 			return "", err
// 		}
// 		return strings.ReplaceAll(path, "./", cwd), nil
// 	}
// 	return path, nil
// }