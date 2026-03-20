package aws

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sso"
	"github.com/takah/sshm/internal/config"
)

// SSOSession represents an SSO session configuration.
type SSOSession struct {
	Name     string
	StartURL string
	Region   string
}

// SSOAccount represents an AWS account accessible via SSO.
type SSOAccount struct {
	AccountID   string
	AccountName string
}

// SSORole represents a role available in an SSO account.
type SSORole struct {
	RoleName  string
	AccountID string
}

// SSOTokenCache represents the cached SSO token file.
type SSOTokenCache struct {
	AccessToken string `json:"accessToken"`
	ExpiresAt   string `json:"expiresAt"`
	Region      string `json:"region"`
	StartURL    string `json:"startUrl"`
}

// FindSSOSessions reads sso-session sections from ~/.aws/config.
func FindSSOSessions() ([]SSOSession, error) {
	configPath := os.Getenv("AWS_CONFIG_FILE")
	if configPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		configPath = filepath.Join(home, ".aws", "config")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", configPath, err)
	}

	var sessions []SSOSession
	var current *SSOSession

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[sso-session ") {
			name := strings.TrimSuffix(strings.TrimPrefix(line, "[sso-session "), "]")
			current = &SSOSession{Name: name}
		} else if strings.HasPrefix(line, "[") {
			if current != nil && current.StartURL != "" {
				sessions = append(sessions, *current)
			}
			current = nil
		} else if current != nil {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			switch key {
			case "sso_start_url":
				current.StartURL = val
			case "sso_region":
				current.Region = val
			}
		}
	}
	if current != nil && current.StartURL != "" {
		sessions = append(sessions, *current)
	}

	return sessions, nil
}

// LoadSSOToken loads the cached access token for the given SSO session.
func LoadSSOToken(session SSOSession) (*SSOTokenCache, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	cacheDir := filepath.Join(home, ".aws", "sso", "cache")

	// Try session name hash first (newer format), then start URL hash
	candidates := []string{
		fmt.Sprintf("%x", sha1.Sum([]byte(session.Name))),
		fmt.Sprintf("%x", sha1.Sum([]byte(session.StartURL))),
	}

	for _, hash := range candidates {
		path := filepath.Join(cacheDir, hash+".json")
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var token SSOTokenCache
		if err := json.Unmarshal(data, &token); err != nil {
			continue
		}
		if token.AccessToken == "" {
			continue
		}

		// Check expiry
		expiry, err := time.Parse(time.RFC3339, token.ExpiresAt)
		if err != nil {
			expiry, err = time.Parse("2006-01-02T15:04:05Z", token.ExpiresAt)
		}
		if err == nil && time.Now().After(expiry) {
			return nil, fmt.Errorf("SSO token expired. Run: aws sso login --sso-session %s", session.Name)
		}

		return &token, nil
	}

	return nil, fmt.Errorf("no cached SSO token found. Run: aws sso login --sso-session %s", session.Name)
}

// ListSSOAccounts lists all accounts accessible with the given SSO token.
func ListSSOAccounts(ctx context.Context, token *SSOTokenCache) ([]SSOAccount, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(token.Region),
	)
	if err != nil {
		return nil, err
	}

	client := sso.NewFromConfig(cfg)
	var accounts []SSOAccount

	paginator := sso.NewListAccountsPaginator(client, &sso.ListAccountsInput{
		AccessToken: aws.String(token.AccessToken),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing SSO accounts: %w", err)
		}
		for _, acct := range page.AccountList {
			accounts = append(accounts, SSOAccount{
				AccountID:   aws.ToString(acct.AccountId),
				AccountName: aws.ToString(acct.AccountName),
			})
		}
	}

	return accounts, nil
}

// ListSSORoles lists roles available for the given account.
func ListSSORoles(ctx context.Context, token *SSOTokenCache, accountID string) ([]SSORole, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(token.Region),
	)
	if err != nil {
		return nil, err
	}

	client := sso.NewFromConfig(cfg)
	var roles []SSORole

	paginator := sso.NewListAccountRolesPaginator(client, &sso.ListAccountRolesInput{
		AccessToken: aws.String(token.AccessToken),
		AccountId:   aws.String(accountID),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("listing SSO roles: %w", err)
		}
		for _, role := range page.RoleList {
			roles = append(roles, SSORole{
				RoleName:  aws.ToString(role.RoleName),
				AccountID: accountID,
			})
		}
	}

	return roles, nil
}

// CommonRegions is the list of AWS regions to offer for selection.
var CommonRegions = []string{
	"ap-northeast-1",
	"ap-northeast-2",
	"ap-northeast-3",
	"ap-southeast-1",
	"ap-southeast-2",
	"ap-south-1",
	"us-east-1",
	"us-east-2",
	"us-west-1",
	"us-west-2",
	"eu-west-1",
	"eu-west-2",
	"eu-west-3",
	"eu-central-1",
	"eu-north-1",
	"ca-central-1",
	"sa-east-1",
}

// DiscoverInstancesWithSSO discovers instances using SSO credentials directly.
func DiscoverInstancesWithSSO(ctx context.Context, token *SSOTokenCache, accountID, roleName, region string) ([]Instance, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(token.Region),
	)
	if err != nil {
		return nil, err
	}

	// Get role credentials
	ssoClient := sso.NewFromConfig(cfg)
	creds, err := ssoClient.GetRoleCredentials(ctx, &sso.GetRoleCredentialsInput{
		AccessToken: aws.String(token.AccessToken),
		AccountId:   aws.String(accountID),
		RoleName:    aws.String(roleName),
	})
	if err != nil {
		return nil, fmt.Errorf("getting role credentials: %w", err)
	}

	// Build config with the obtained credentials
	staticCreds := aws.CredentialsProviderFunc(func(ctx context.Context) (aws.Credentials, error) {
		return aws.Credentials{
			AccessKeyID:     aws.ToString(creds.RoleCredentials.AccessKeyId),
			SecretAccessKey: aws.ToString(creds.RoleCredentials.SecretAccessKey),
			SessionToken:    aws.ToString(creds.RoleCredentials.SessionToken),
			Source:          "SSOGetRoleCredentials",
		}, nil
	})

	instCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(staticCreds),
	)
	if err != nil {
		return nil, err
	}

	dummyProf := config.SSOProfile{
		Name:      fmt.Sprintf("%s/%s", accountID, roleName),
		AccountID: accountID,
		RoleName:  roleName,
		Region:    region,
	}

	return discoverWithConfig(ctx, instCfg, dummyProf)
}
