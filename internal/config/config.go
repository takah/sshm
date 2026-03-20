package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/ini.v1"
)

// SSOProfile represents an AWS SSO profile from ~/.aws/config.
type SSOProfile struct {
	Name      string // profile name (e.g., "dev-admin")
	AccountID string
	RoleName  string
	Region    string // region for this profile (falls back to sso_session region)
	SSORegion string // SSO endpoint region
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

		// Fall back to sso-session region
		if ssoRegion == "" {
			if sessionName := section.Key("sso_session").String(); sessionName != "" {
				ssoRegion = ssoSessionRegions[sessionName]
			}
		}
		if region == "" {
			region = ssoRegion
		}

		profiles = append(profiles, SSOProfile{
			Name:      profileName,
			AccountID: accountID,
			RoleName:  roleName,
			Region:    region,
			SSORegion: ssoRegion,
		})
	}

	return profiles, nil
}
