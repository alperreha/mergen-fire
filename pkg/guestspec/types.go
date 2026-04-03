package guestspec

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type ImageMeta struct {
	Image             string    `json:"image"`
	CreatedAt         time.Time `json:"createdAt,omitempty"`
	Entrypoint        []string  `json:"entrypoint,omitempty"`
	Cmd               []string  `json:"cmd,omitempty"`
	StartCmd          []string  `json:"startCmd,omitempty"`
	Env               []string  `json:"env,omitempty"`
	WorkingDir        string    `json:"workingDir,omitempty"`
	User              string    `json:"user,omitempty"`
	ExposedPorts      []string  `json:"exposedPorts,omitempty"`
	SuggestedHTTPPort int       `json:"suggestedHTTPPort,omitempty"`
}

type Runtime struct {
	Image             string   `json:"image"`
	BootArgs          string   `json:"bootArgs,omitempty"`
	HTTPPort          int      `json:"httpPort,omitempty"`
	Entrypoint        []string `json:"entrypoint,omitempty"`
	Cmd               []string `json:"cmd,omitempty"`
	StartCmd          []string `json:"startCmd,omitempty"`
	Env               []string `json:"env,omitempty"`
	WorkingDir        string   `json:"workingDir,omitempty"`
	User              string   `json:"user,omitempty"`
	AgentDevice       string   `json:"agentDevice,omitempty"`
	AgentFSType       string   `json:"agentFSType,omitempty"`
	AgentMountPoint   string   `json:"agentMountPoint,omitempty"`
	AgentReadOnly     bool     `json:"agentReadOnly,omitempty"`
	AgentPath         string   `json:"agentPath,omitempty"`
	PayloadDevice     string   `json:"payloadDevice,omitempty"`
	PayloadFSType     string   `json:"payloadFSType,omitempty"`
	PayloadMountPoint string   `json:"payloadMountPoint,omitempty"`
	PayloadReadOnly   bool     `json:"payloadReadOnly,omitempty"`
	EnvDevice         string   `json:"envDevice,omitempty"`
	EnvFSType         string   `json:"envFSType,omitempty"`
	EnvMountPoint     string   `json:"envMountPoint,omitempty"`
	EnvReadOnly       bool     `json:"envReadOnly,omitempty"`
	EnvFile           string   `json:"envFile,omitempty"`
	VSockEnabled      bool     `json:"vsockEnabled,omitempty"`
	VSockGuestPath    string   `json:"vsockGuestPath,omitempty"`
	VSockShell        string   `json:"vsockShell,omitempty"`
	VSockAuthToken    string   `json:"vsockAuthToken,omitempty"`
	VSockDebug        bool     `json:"vsockDebug,omitempty"`
}

func ReadImageMeta(path string) (ImageMeta, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return ImageMeta{}, fmt.Errorf("read image metadata: %w", err)
	}

	var meta ImageMeta
	if err := json.Unmarshal(body, &meta); err != nil {
		return ImageMeta{}, fmt.Errorf("decode image metadata: %w", err)
	}
	return meta, nil
}

func ReadRuntime(path string) (Runtime, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return Runtime{}, fmt.Errorf("read runtime metadata: %w", err)
	}

	var spec Runtime
	if err := json.Unmarshal(body, &spec); err != nil {
		return Runtime{}, fmt.Errorf("decode runtime metadata: %w", err)
	}
	return spec, nil
}
