package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config holds the terminal configuration
type Config struct {
	Shell          string            `json:"shell"`
	CustomCommands []CustomCommand   `json:"custom_commands"`
	Aliases        map[string]string `json:"aliases"`
}

// CustomCommand represents a user-defined command
type CustomCommand struct {
	Name        string `json:"name"`
	Command     string `json:"command"`
	Description string `json:"description"`
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		Shell:          "",
		CustomCommands: []CustomCommand{},
		Aliases:        make(map[string]string),
	}
}

// GetConfigPath returns the path to the config file
func GetConfigPath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ".raven_terminal.json"
	}
	configDir := filepath.Join(homeDir, ".config", "raven-terminal")
	os.MkdirAll(configDir, 0755)
	return filepath.Join(configDir, "config.json")
}

// Load loads the configuration from disk
func Load() (*Config, error) {
	configPath := GetConfigPath()
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(), nil
		}
		return nil, err
	}

	cfg := DefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Save saves the configuration to disk
func (c *Config) Save() error {
	configPath := GetConfigPath()
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, data, 0644)
}

// GetAvailableShells returns a list of available shells on the system
func GetAvailableShells() []string {
	shells := []string{}
	possibleShells := []string{
		"/bin/bash",
		"/usr/bin/bash",
		"/bin/zsh",
		"/usr/bin/zsh",
		"/bin/fish",
		"/usr/bin/fish",
		"/bin/sh",
		"/usr/bin/sh",
		"/bin/dash",
		"/usr/bin/dash",
		"/bin/tcsh",
		"/usr/bin/tcsh",
		"/bin/ksh",
		"/usr/bin/ksh",
	}

	seen := make(map[string]bool)
	for _, shell := range possibleShells {
		if _, err := os.Stat(shell); err == nil {
			// Get the base name for deduplication
			base := filepath.Base(shell)
			if !seen[base] {
				seen[base] = true
				shells = append(shells, shell)
			}
		}
	}
	return shells
}

// AddCustomCommand adds a new custom command
func (c *Config) AddCustomCommand(name, command, description string) {
	c.CustomCommands = append(c.CustomCommands, CustomCommand{
		Name:        name,
		Command:     command,
		Description: description,
	})
}

// RemoveCustomCommand removes a custom command by index
func (c *Config) RemoveCustomCommand(index int) {
	if index >= 0 && index < len(c.CustomCommands) {
		c.CustomCommands = append(c.CustomCommands[:index], c.CustomCommands[index+1:]...)
	}
}

// SetAlias sets an alias
func (c *Config) SetAlias(name, command string) {
	if c.Aliases == nil {
		c.Aliases = make(map[string]string)
	}
	c.Aliases[name] = command
}

// RemoveAlias removes an alias
func (c *Config) RemoveAlias(name string) {
	delete(c.Aliases, name)
}
