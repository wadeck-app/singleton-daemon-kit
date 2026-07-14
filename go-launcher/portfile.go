package launcher

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type portFile struct {
	Port int `json:"port"`
	Pid  int `json:"pid"`
}

func readPortFile(configDir string) (portFile, error) {
	data, err := os.ReadFile(filepath.Join(configDir, "config.port"))
	if err != nil {
		return portFile{}, fmt.Errorf("config.port not found in %s — daemon is not running", configDir)
	}
	var pf portFile
	if err := json.Unmarshal(data, &pf); err != nil {
		return portFile{}, fmt.Errorf("config.port is malformed: %w", err)
	}
	if pf.Port == 0 {
		return portFile{}, fmt.Errorf("config.port contains port=0 — daemon is not running")
	}
	return pf, nil
}

func readHealthToken(configDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(configDir, "health_token"))
	if err != nil {
		return "", fmt.Errorf("health_token not found in %s", configDir)
	}
	token := string(data)
	for len(token) > 0 && (token[len(token)-1] == '\n' || token[len(token)-1] == '\r') {
		token = token[:len(token)-1]
	}
	return token, nil
}
