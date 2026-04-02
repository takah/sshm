package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	ssmaws "github.com/takah/sshm/internal/aws"
	"github.com/takah/sshm/internal/cache"
	"github.com/takah/sshm/internal/config"
	"github.com/takah/sshm/internal/ui"
)

// isDocumentListMode returns true when -d/--document is given without a value.
func isDocumentListMode() bool {
	args := os.Args[1:]
	for i, arg := range args {
		if arg == "-d" || arg == "--document" {
			return i+1 >= len(args) || strings.HasPrefix(args[i+1], "-")
		}
	}
	return false
}

func printDocumentList(cfg *config.SSHMConfig) {
	fmt.Printf("Config: %s\n\n", cfg.Path)
	if len(cfg.Documents) == 0 {
		fmt.Println("No documents configured.")
		fmt.Println("Add entries to your config file:")
		fmt.Println("")
		fmt.Println("  documents:")
		fmt.Println("    admin: SSM-SessionManagerRunShellAsAdmin")
		return
	}
	fmt.Println("Configured documents:")
	for short, full := range cfg.Documents {
		fmt.Printf("  %-20s %s\n", short, full)
	}
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	sshmCfg, err := config.LoadSSHMConfig()
	if err != nil {
		return err
	}

	// Handle -d/--document without a value before flag.Parse() to avoid parse error.
	if isDocumentListMode() {
		printDocumentList(sshmCfg)
		return nil
	}

	discover := flag.Bool("D", false, "Discover accounts/roles via SSO API (no config profiles needed)")
	flag.BoolVar(discover, "discover", false, "Discover accounts/roles via SSO API (no config profiles needed)")
	documentArg := flag.String("d", "", "SSM document name or short name (defined in ~/.config/sshm/config.yml)")
	flag.StringVar(documentArg, "document", "", "SSM document name or short name (defined in ~/.config/sshm/config.yml)")
	noCache := flag.Bool("update-cache", false, "Refresh cached instance list")
	clearCache := flag.Bool("clear-cache", false, "Clear all cached data and exit")
	flag.Parse()

	if *clearCache {
		if err := cache.Clear(); err != nil {
			return fmt.Errorf("clearing cache: %w", err)
		}
		fmt.Println("Cache cleared.")
		return nil
	}

	documentName := sshmCfg.ResolveDocument(*documentArg)

	nameFilter := flag.Arg(0)

	if *discover {
		return runDiscoverMode(nameFilter, *noCache, documentName)
	}
	return runProfileMode(nameFilter, *noCache, documentName)
}

// runProfileMode uses ~/.aws/config profiles to find instances.
func runProfileMode(nameFilter string, noCache bool, documentName string) error {
	profiles, err := config.LoadSSOProfiles()
	if err != nil {
		return fmt.Errorf("loading AWS profiles: %w", err)
	}
	if len(profiles) == 0 {
		fmt.Println("No SSO profiles found in ~/.aws/config.")
		fmt.Println("Tip: Use 'sshm -d' to discover accounts via SSO API without profiles.")
		return fmt.Errorf("no SSO profiles found")
	}

	// Build cache key from sorted profile names
	names := make([]string, len(profiles))
	for i, p := range profiles {
		names[i] = p.Name
	}
	sort.Strings(names)
	cacheKey := "profiles_" + strings.Join(names, "_")

	instances, err := loadOrDiscover(cacheKey, noCache, func() ([]ssmaws.Instance, error) {
		msg := fmt.Sprintf("Searching %d profiles...", len(profiles))
		sp := ui.NewSpinner(msg)
		defer sp.Stop()

		return ssmaws.DiscoverInstances(profiles)
	})
	if err != nil {
		return fmt.Errorf("discovering instances: %w", err)
	}
	if len(instances) == 0 {
		return fmt.Errorf("no SSM-managed instances found")
	}

	return selectAndConnect(instances, nameFilter, documentName)
}

// runDiscoverMode uses SSO API to interactively pick account/role/region, then find instances.
func runDiscoverMode(nameFilter string, noCache bool, documentName string) error {
	ctx := context.Background()

	// 1. Find SSO sessions
	sessions, err := ssmaws.FindSSOSessions()
	if err != nil || len(sessions) == 0 {
		return fmt.Errorf("no SSO sessions found in ~/.aws/config. Add an [sso-session] section first")
	}

	// Pick SSO session if multiple
	session := sessions[0]
	if len(sessions) > 1 {
		items := make([]ui.PickerItem, len(sessions))
		for i, s := range sessions {
			items[i] = ui.PickerItem{
				ID:      s.Name,
				Display: fmt.Sprintf("%-20s %s", s.Name, s.StartURL),
				Search:  s.Name + " " + s.StartURL,
			}
		}
		picked, err := ui.Pick("Select SSO session", items)
		if err != nil {
			return err
		}
		for _, s := range sessions {
			if s.Name == picked {
				session = s
				break
			}
		}
	}

	// 2. Load SSO token
	token, err := ssmaws.LoadSSOToken(session)
	if err != nil {
		return err
	}

	// 3. List and pick account
	accounts, err := ssmaws.ListSSOAccounts(ctx, token)
	if err != nil {
		return err
	}
	if len(accounts) == 0 {
		return fmt.Errorf("no accounts accessible")
	}

	var accountID string
	if len(accounts) == 1 {
		accountID = accounts[0].AccountID
		fmt.Printf("Account: %s (%s)\n", accounts[0].AccountName, accountID)
	} else {
		items := make([]ui.PickerItem, len(accounts))
		for i, a := range accounts {
			items[i] = ui.PickerItem{
				ID:      a.AccountID,
				Display: fmt.Sprintf("%-40s %s", a.AccountName, a.AccountID),
				Search:  a.AccountName + " " + a.AccountID,
			}
		}
		accountID, err = ui.Pick("Select account", items)
		if err != nil {
			return err
		}
	}

	// 4. List and pick role
	roles, err := ssmaws.ListSSORoles(ctx, token, accountID)
	if err != nil {
		return err
	}
	if len(roles) == 0 {
		return fmt.Errorf("no roles available for account %s", accountID)
	}

	var roleName string
	if len(roles) == 1 {
		roleName = roles[0].RoleName
		fmt.Printf("Role: %s\n", roleName)
	} else {
		items := make([]ui.PickerItem, len(roles))
		for i, r := range roles {
			items[i] = ui.PickerItem{
				ID:      r.RoleName,
				Display: r.RoleName,
				Search:  r.RoleName,
			}
		}
		roleName, err = ui.Pick("Select role", items)
		if err != nil {
			return err
		}
	}

	// 5. Pick region
	items := make([]ui.PickerItem, len(ssmaws.CommonRegions))
	for i, r := range ssmaws.CommonRegions {
		items[i] = ui.PickerItem{
			ID:      r,
			Display: r,
			Search:  r,
		}
	}
	region, err := ui.Pick("Select region", items)
	if err != nil {
		return err
	}

	// 6. Discover instances (with cache)
	cacheKey := fmt.Sprintf("sso_%s_%s_%s_%s", session.Name, accountID, roleName, region)

	instances, err := loadOrDiscover(cacheKey, noCache, func() ([]ssmaws.Instance, error) {
		sp := ui.NewSpinner(fmt.Sprintf("Searching instances in %s (%s)...", accountID, region))
		defer sp.Stop()

		return ssmaws.DiscoverInstancesWithSSO(ctx, token, accountID, roleName, region)
	})
	if err != nil {
		return fmt.Errorf("discovering instances: %w", err)
	}
	if len(instances) == 0 {
		return fmt.Errorf("no SSM-managed instances found")
	}

	return selectAndConnect(instances, nameFilter, documentName)
}

// loadOrDiscover tries to load instances from cache, falling back to the discover function.
func loadOrDiscover(cacheKey string, noCache bool, discover func() ([]ssmaws.Instance, error)) ([]ssmaws.Instance, error) {
	// Try cache first
	if !noCache {
		if data, err := cache.Load(cacheKey); err == nil && data != nil {
			var instances []ssmaws.Instance
			if err := json.Unmarshal(data, &instances); err == nil && len(instances) > 0 {
				fmt.Printf("Using cached data (%d instances). Use --update-cache to refresh.\n", len(instances))
				return instances, nil
			}
		}
	}

	// Discover
	instances, err := discover()
	if err != nil {
		return nil, err
	}

	// Save to cache
	if data, err := json.Marshal(instances); err == nil {
		_ = cache.Save(cacheKey, data)
	}

	return instances, nil
}

func selectAndConnect(instances []ssmaws.Instance, nameFilter string, documentName string) error {
	if nameFilter != "" {
		instances = ssmaws.FilterByName(instances, nameFilter)
		if len(instances) == 0 {
			return fmt.Errorf("no instances matching %q", nameFilter)
		}
		if len(instances) == 1 {
			return ssmaws.StartSession(instances[0], documentName)
		}
	}

	selected, err := ui.SelectInstance(instances)
	if err != nil {
		return err
	}
	return ssmaws.StartSession(selected, documentName)
}
