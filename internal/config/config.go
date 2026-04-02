package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/ini.v1"
	"gopkg.in/yaml.v3"
)

// SSHMConfig holds sshm-specific configuration from ~/.config/sshm/config.yml.
type SSHMConfig struct {
	Documents map[string]string `yaml:"documents"`
	Path      string            `yaml:"-"` // path to the config file (set by LoadSSHMConfig)
}

// LoadSSHMConfig reads ~/.config/sshm/config.yml.
// Returns an empty config if the file does not exist.
func LoadSSHMConfig() (*SSHMConfig, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return &SSHMConfig{Documents: map[string]string{}}, nil
	}
	path := filepath.Join(home, ".config", "sshm", "config.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &SSHMConfig{Documents: map[string]string{}, Path: path}, nil
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var cfg SSHMConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	if cfg.Documents == nil {
		cfg.Documents = map[string]string{}
	}
	cfg.Path = path
	return &cfg, nil
}

// ResolveDocument returns the full document name for the given short name.
// If the name is not in the map, it is returned as-is.
func (c *SSHMConfig) ResolveDocument(name string) string {
	if full, ok := c.Documents[name]; ok {
		return full
	}
	return name
}

// SSOProfile represents an AWS SSO profile from ~/.aws/config.
type SSOProfile struct {
	Name       string // profile name (e.g., "dev-admin")
	AccountID  string
	RoleName   string
	Region     string // region for this profile (falls back to sso_session region)
	SSORegion  string // SSO endpoint region
	SSOSession string // sso-session name (for login hint)
}

// LoadSSOProfiles parses ~/.aws/config and returns all SSO-configured profiles.
func LoadSSOProfiles() ([]SSOProfile, error) {
	configPath := os.Getenv("AWS_CONFIG_FILE")
	if configPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		configPath = filepath.Join(home, ".aws", "config")
	}

	cfg, err := ini.Load(configPath)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", configPath, err)
	}

	// Collect sso-session regions for fallback
	ssoSessionRegions := map[string]string{}
	for _, section := range cfg.Sections() {
		name := section.Name()
		if strings.HasPrefix(name, "sso-session ") {
			sessionName := strings.TrimPrefix(name, "sso-session ")
			if r := section.Key("sso_region").String(); r != "" {
				ssoSessionRegions[sessionName] = r
			}
		}
	}

	var profiles []SSOProfile
	for _, section := range cfg.Sections() {
		name := section.Name()

		// Profile sections are "profile xxx" (or "default")
		var profileName string
		if strings.HasPrefix(name, "profile ") {
			profileName = strings.TrimPrefix(name, "profile ")
		} else if name == "default" {
			profileName = "default"
		} else {
			continue
		}

		accountID := section.Key("sso_account_id").String()
		roleName := section.Key("sso_role_name").String()
		if accountID == "" || roleName == "" {
			continue // not an SSO profile
		}

		region := section.Key("region").String()
		ssoRegion := section.Key("sso_region").String()
		ssoSession := section.Key("sso_session").String()

		// Fall back to sso-session region
		if ssoRegion == "" {
			if ssoSession != "" {
				ssoRegion = ssoSessionRegions[ssoSession]
			}
		}
		if region == "" {
			region = ssoRegion
		}

		profiles = append(profiles, SSOProfile{
			Name:       profileName,
			AccountID:  accountID,
			RoleName:   roleName,
			Region:     region,
			SSORegion:  ssoRegion,
			SSOSession: ssoSession,
		})
	}

	return profiles, nil
}
