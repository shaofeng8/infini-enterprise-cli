package config

import "os"

func writeFile(path, contents string) error {
	return os.WriteFile(path, []byte(contents), 0o600)
}
