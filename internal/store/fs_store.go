package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/alperreha/mergen-fire/internal/model"
)

var ErrNotFound = errors.New("vm not found")

type FSStore struct {
	configRoot string
	dataRoot   string
	runRoot    string
	hooksRoot  string
	logger     *slog.Logger
}

func NewFSStore(configRoot, dataRoot, runRoot, hooksRoot string) *FSStore {
	return &FSStore{
		configRoot: configRoot,
		dataRoot:   dataRoot,
		runRoot:    runRoot,
		hooksRoot:  hooksRoot,
		logger:     slog.Default(),
	}
}

func (s *FSStore) WithLogger(logger *slog.Logger) *FSStore {
	if logger != nil {
		s.logger = logger
	}
	return s
}

func (s *FSStore) EnsureBaseDirs() error {
	s.logger.Debug("ensuring store base directories", "configRoot", s.configRoot, "dataRoot", s.dataRoot, "runRoot", s.runRoot)
	dirs := []string{s.configRoot, s.dataRoot, s.runRoot}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return err
		}
	}
	return nil
}

func (s *FSStore) SaveVM(id string, cfg model.VMConfig, meta model.VMMetadata, hooks model.HooksConfig, env map[string]string) (model.VMPaths, error) {
	if err := validateID(id); err != nil {
		return model.VMPaths{}, err
	}
	s.logger.Debug("saving vm artifacts", "vmID", id, "ports", len(meta.Ports), "hasHooks", hasHooks(hooks), "envCount", len(env))

	paths := s.PathsFor(id)
	meta.Paths = paths

	dirs := []string{
		paths.ConfigDir,
		paths.RunDir,
		paths.DataDir,
		paths.LogsDir,
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return model.VMPaths{}, err
		}
	}

	if err := writeJSONAtomic(paths.VMConfigPath, cfg, 0o640); err != nil {
		return model.VMPaths{}, err
	}
	if err := writeJSONAtomic(paths.MetaPath, meta, 0o640); err != nil {
		return model.VMPaths{}, err
	}

	if hasHooks(hooks) {
		if err := writeJSONAtomic(paths.HooksPath, hooks, 0o640); err != nil {
			return model.VMPaths{}, err
		}
	}

	if len(env) > 0 {
		if err := writeEnvAtomic(paths.EnvPath, env, 0o640); err != nil {
			return model.VMPaths{}, err
		}
	}

	s.logger.Debug("vm artifacts saved", "vmID", id, "configDir", paths.ConfigDir)
	return paths, nil
}

func (s *FSStore) Exists(id string) (bool, error) {
	if err := validateID(id); err != nil {
		return false, err
	}
	metaPath := s.PathsFor(id).MetaPath
	_, err := os.Stat(metaPath)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func (s *FSStore) ReadMeta(id string) (model.VMMetadata, error) {
	if err := validateID(id); err != nil {
		return model.VMMetadata{}, err
	}
	s.logger.Debug("reading vm metadata", "vmID", id)
	var meta model.VMMetadata
	if err := readJSON(s.PathsFor(id).MetaPath, &meta); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return model.VMMetadata{}, ErrNotFound
		}
		return model.VMMetadata{}, err
	}
	return meta, nil
}

func (s *FSStore) ReadVMConfig(id string) (model.VMConfig, error) {
	if err := validateID(id); err != nil {
		return model.VMConfig{}, err
	}
	s.logger.Debug("reading vm config", "vmID", id)
	var cfg model.VMConfig
	if err := readJSON(s.PathsFor(id).VMConfigPath, &cfg); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return model.VMConfig{}, ErrNotFound
		}
		return model.VMConfig{}, err
	}
	return cfg, nil
}

func (s *FSStore) ReadHooks(id string) (model.HooksConfig, error) {
	if err := validateID(id); err != nil {
		return model.HooksConfig{}, err
	}
	s.logger.Debug("reading vm hooks", "vmID", id)
	var hooks model.HooksConfig
	if err := readJSON(s.PathsFor(id).HooksPath, &hooks); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return model.HooksConfig{}, nil
		}
		return model.HooksConfig{}, err
	}
	return hooks, nil
}

func (s *FSStore) ReadGlobalHooks() (model.HooksConfig, error) {
	var merged model.HooksConfig
	if s.hooksRoot == "" {
		return merged, nil
	}
	s.logger.Debug("reading global hooks", "hooksRoot", s.hooksRoot)

	entries, err := os.ReadDir(s.hooksRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return merged, nil
		}
		return merged, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		fullPath := filepath.Join(s.hooksRoot, entry.Name())
		var hooks model.HooksConfig
		if err := readJSON(fullPath, &hooks); err != nil {
			return merged, fmt.Errorf("read global hook file %s: %w", entry.Name(), err)
		}

		merged.OnCreate = append(merged.OnCreate, hooks.OnCreate...)
		merged.OnDelete = append(merged.OnDelete, hooks.OnDelete...)
		merged.OnStart = append(merged.OnStart, hooks.OnStart...)
		merged.OnStop = append(merged.OnStop, hooks.OnStop...)
	}

	s.logger.Debug(
		"global hooks loaded",
		"onCreate", len(merged.OnCreate),
		"onDelete", len(merged.OnDelete),
		"onStart", len(merged.OnStart),
		"onStop", len(merged.OnStop),
	)
	return merged, nil
}

func (s *FSStore) ListVMIDs() ([]string, error) {
	s.logger.Debug("listing vm ids", "configRoot", s.configRoot)
	entries, err := os.ReadDir(s.configRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}

	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		ids = append(ids, entry.Name())
	}
	sort.Strings(ids)
	s.logger.Debug("listed vm ids", "count", len(ids))
	return ids, nil
}

func (s *FSStore) ListMetas() ([]model.VMMetadata, error) {
	ids, err := s.ListVMIDs()
	if err != nil {
		return nil, err
	}

	metas := make([]model.VMMetadata, 0, len(ids))
	for _, id := range ids {
		meta, readErr := s.ReadMeta(id)
		if readErr != nil {
			if errors.Is(readErr, ErrNotFound) {
				continue
			}
			return nil, readErr
		}
		metas = append(metas, meta)
	}
	return metas, nil
}

func (s *FSStore) DeleteVM(id string, retainData bool) error {
	if err := validateID(id); err != nil {
		return err
	}
	s.logger.Debug("deleting vm from store", "vmID", id, "retainData", retainData)

	exists, err := s.Exists(id)
	if err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}

	paths := s.PathsFor(id)
	if err := os.RemoveAll(paths.ConfigDir); err != nil {
		return err
	}
	if err := os.RemoveAll(paths.RunDir); err != nil {
		return err
	}
	if !retainData {
		if err := os.RemoveAll(paths.DataDir); err != nil {
			return err
		}
	}
	s.logger.Debug("vm deleted from store", "vmID", id)
	return nil
}

func (s *FSStore) PathsFor(id string) model.VMPaths {
	configDir := filepath.Join(s.configRoot, id)
	dataDir := filepath.Join(s.dataRoot, id)
	runDir := filepath.Join(s.runRoot, id)

	return model.VMPaths{
		ConfigDir:    configDir,
		VMConfigPath: filepath.Join(configDir, "vm.json"),
		MetaPath:     filepath.Join(configDir, "meta.json"),
		HooksPath:    filepath.Join(configDir, "hooks.json"),
		EnvPath:      filepath.Join(configDir, "env"),
		RunDir:       runDir,
		SocketPath:   filepath.Join(runDir, "firecracker.socket"),
		LockPath:     filepath.Join(s.runRoot, id+".lock"),
		DataDir:      dataDir,
		LogsDir:      filepath.Join(dataDir, "logs"),
	}
}

func hasHooks(h model.HooksConfig) bool {
	return len(h.OnCreate) > 0 || len(h.OnDelete) > 0 || len(h.OnStart) > 0 || len(h.OnStop) > 0
}

func writeJSONAtomic(path string, payload any, mode os.FileMode) error {
	content, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	return writeAtomic(path, content, mode)
}

func writeEnvAtomic(path string, env map[string]string, mode os.FileMode) error {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, fmt.Sprintf("%s=%s", key, shellEscape(env[key])))
	}
	content := []byte(strings.Join(lines, "\n") + "\n")
	return writeAtomic(path, content, mode)
}

func writeAtomic(path string, content []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}

	tempFile, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()

	if _, err := tempFile.Write(content); err != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
		return err
	}
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	if err := os.Chmod(tempPath, mode); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}

func readJSON(path string, out any) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(content) == 0 {
		return nil
	}
	return json.Unmarshal(content, out)
}

func validateID(id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("vm id is empty")
	}
	if strings.Contains(id, "/") || strings.Contains(id, "..") {
		return errors.New("vm id is invalid")
	}
	return nil
}

func shellEscape(value string) string {
	escaped := strings.ReplaceAll(value, "'", "'\\''")
	return fmt.Sprintf("'%s'", escaped)
}
