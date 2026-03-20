package aws

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

// StartSession starts an SSM session to the given instance.
// If the instance has a named profile, uses aws cli directly.
// Otherwise, starts the session via the SDK and session-manager-plugin.
func StartSession(inst Instance) error {
	fmt.Printf("Connecting to %s (%s) via %s...\n",
		inst.Name, inst.InstanceID, inst.Profile.Name)

	// If we have a real profile name (not "accountID/roleName"), use aws cli
	if inst.Profile.Name != "" && inst.Profile.RoleName == "" {
		return startWithCLI(inst)
	}
	if inst.Profile.Name != "" && !isDiscoverProfile(inst.Profile.Name) {
		return startWithCLI(inst)
	}

	// Discover mode: use SDK to start session
	return startWithSDK(inst)
}

func isDiscoverProfile(name string) bool {
	// Discover mode profiles look like "123456789012/RoleName"
	for _, c := range name {
		if c == '/' {
			return true
		}
	}
	return false
}

func startWithCLI(inst Instance) error {
	awsBin, err := exec.LookPath("aws")
	if err != nil {
		return fmt.Errorf("aws cli not found in PATH: %w", err)
	}

	args := []string{
		"aws", "ssm", "start-session",
		"--target", inst.InstanceID,
		"--profile", inst.Profile.Name,
	}
	if inst.Profile.Region != "" {
		args = append(args, "--region", inst.Profile.Region)
	}

	return syscall.Exec(awsBin, args, os.Environ())
}

func startWithSDK(inst Instance) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(inst.Profile.Region),
	)
	if err != nil {
		return err
	}

	ssmClient := ssm.NewFromConfig(cfg)
	out, err := ssmClient.StartSession(ctx, &ssm.StartSessionInput{
		Target: &inst.InstanceID,
	})
	if err != nil {
		return fmt.Errorf("starting session: %w", err)
	}

	// session-manager-plugin needs the session info as JSON
	sessionJSON, _ := json.Marshal(out)
	inputJSON, _ := json.Marshal(map[string]string{
		"Target": inst.InstanceID,
	})

	pluginBin, err := exec.LookPath("session-manager-plugin")
	if err != nil {
		return fmt.Errorf("session-manager-plugin not found in PATH: %w", err)
	}

	endpoint := fmt.Sprintf("https://ssm.%s.amazonaws.com", inst.Profile.Region)

	return syscall.Exec(pluginBin, []string{
		"session-manager-plugin",
		string(sessionJSON),
		inst.Profile.Region,
		"StartSession",
		"",
		string(inputJSON),
		endpoint,
	}, os.Environ())
}
