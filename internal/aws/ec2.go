package aws

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/takah/sshm/internal/config"
)

// Instance represents an EC2 instance that is reachable via SSM.
type Instance struct {
	InstanceID   string
	Name         string
	PrivateIP    string
	InstanceType string
	State        string
	Profile      config.SSOProfile // which AWS profile to use for connection
}

// DiscoverInstances finds SSM-managed EC2 instances across all given profiles.
// If an SSO token error is detected, it cancels all in-flight requests immediately.
func DiscoverInstances(profiles []config.SSOProfile) ([]Instance, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		mu         sync.Mutex
		all        []Instance
		errs       []string
		ssoErr     bool
		ssoSession string // SSO session name for login hint
		wg         sync.WaitGroup
	)

	for _, p := range profiles {
		wg.Add(1)
		go func(prof config.SSOProfile) {
			defer wg.Done()

			instances, err := discoverForProfile(ctx, prof)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errMsg := fmt.Sprintf("[%s] %v", prof.Name, err)
				if isSSOTokenError(errMsg) {
					if !ssoErr {
						ssoErr = true
						ssoSession = prof.SSOSession
						cancel() // cancel all other in-flight requests
					}
				} else if ctx.Err() == nil {
					// Only collect non-SSO errors if we haven't cancelled
					errs = append(errs, errMsg)
				}
				return
			}
			all = append(all, instances...)
		}(p)
	}

	wg.Wait()

	// SSO token error — return a single clear message with login command
	if ssoErr {
		if ssoSession != "" {
			return nil, fmt.Errorf("SSO token expired. Run: aws sso login --sso-session %s", ssoSession)
		}
		return nil, fmt.Errorf("SSO token expired. Run: aws sso login")
	}

	// Print warnings for failed profiles but don't fail entirely.
	// Skip "No access" errors when some instances were found — those are expected
	// for profiles the user simply doesn't have permission on.
	var realErrs []string
	for _, e := range errs {
		if !isNoAccessError(e) {
			realErrs = append(realErrs, e)
		}
	}
	if len(realErrs) > 0 && len(all) == 0 {
		return nil, fmt.Errorf("all profiles failed:\n  %s", strings.Join(realErrs, "\n  "))
	}
	for _, e := range realErrs {
		fmt.Printf("Warning: %s\n", e)
	}

	return all, nil
}

// isSSOTokenError checks if an error string indicates an SSO token/credential refresh failure.
// It must distinguish genuine token issues from account-level access denials (e.g.,
// ForbiddenException: No access when the user simply lacks permission on that account/role).
func isSSOTokenError(errStr string) bool {
	lower := strings.ToLower(errStr)

	// Definitive SSO token issues
	if strings.Contains(lower, "refresh cached sso token") ||
		strings.Contains(lower, "cached sso token file") ||
		strings.Contains(lower, "invalidgrantexception") ||
		strings.Contains(lower, "sso token expired") ||
		strings.Contains(lower, "no cached sso token") {
		return true
	}

	// ForbiddenException from SSO GetRoleCredentials can mean either an expired/invalid
	// token OR simply no access to that account/role. Only treat it as an SSO token error
	// if it's NOT a "No access" denial (which indicates a permissions issue, not a token issue).
	if strings.Contains(lower, "failed to refresh cached credentials") &&
		strings.Contains(lower, "forbiddenexception") {
		return !strings.Contains(lower, "no access")
	}

	return false
}

// isNoAccessError returns true when the error is a plain permission denial
// (ForbiddenException: No access) rather than a token or credential issue.
func isNoAccessError(errStr string) bool {
	lower := strings.ToLower(errStr)
	return strings.Contains(lower, "forbiddenexception") &&
		strings.Contains(lower, "no access")
}

func discoverForProfile(ctx context.Context, prof config.SSOProfile) ([]Instance, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithSharedConfigProfile(prof.Name),
		awsconfig.WithRegion(prof.Region),
	)
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}

	return discoverWithConfig(ctx, cfg, prof)
}

// discoverWithConfig finds SSM-managed instances using the given AWS config.
func discoverWithConfig(ctx context.Context, cfg aws.Config, prof config.SSOProfile) ([]Instance, error) {
	// Get SSM-managed instance IDs
	ssmClient := ssm.NewFromConfig(cfg)
	managedIDs, err := getManagedInstanceIDs(ctx, ssmClient)
	if err != nil {
		return nil, fmt.Errorf("listing SSM instances: %w", err)
	}
	if len(managedIDs) == 0 {
		return nil, nil
	}

	// Get EC2 instance details
	ec2Client := ec2.NewFromConfig(cfg)
	return getInstanceDetails(ctx, ec2Client, managedIDs, prof)
}

func getManagedInstanceIDs(ctx context.Context, client *ssm.Client) (map[string]bool, error) {
	ids := make(map[string]bool)
	paginator := ssm.NewDescribeInstanceInformationPaginator(client, &ssm.DescribeInstanceInformationInput{})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, info := range page.InstanceInformationList {
			if info.InstanceId != nil {
				ids[*info.InstanceId] = true
			}
		}
	}
	return ids, nil
}

func getInstanceDetails(ctx context.Context, client *ec2.Client, managedIDs map[string]bool, prof config.SSOProfile) ([]Instance, error) {
	var instances []Instance

	paginator := ec2.NewDescribeInstancesPaginator(client, &ec2.DescribeInstancesInput{
		Filters: []ec2Types.Filter{
			{
				Name:   aws.String("instance-state-name"),
				Values: []string{"running"},
			},
		},
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, reservation := range page.Reservations {
			for _, inst := range reservation.Instances {
				if inst.InstanceId == nil {
					continue
				}
				if !managedIDs[*inst.InstanceId] {
					continue
				}

				name := ""
				for _, tag := range inst.Tags {
					if tag.Key != nil && *tag.Key == "Name" && tag.Value != nil {
						name = *tag.Value
						break
					}
				}

				privateIP := ""
				if inst.PrivateIpAddress != nil {
					privateIP = *inst.PrivateIpAddress
				}

				instanceType := ""
				if inst.InstanceType != "" {
					instanceType = string(inst.InstanceType)
				}

				instances = append(instances, Instance{
					InstanceID:   *inst.InstanceId,
					Name:         name,
					PrivateIP:    privateIP,
					InstanceType: instanceType,
					State:        "running",
					Profile:      prof,
				})
			}
		}
	}

	return instances, nil
}

// FilterByName returns instances whose Name contains the filter string (case-insensitive).
func FilterByName(instances []Instance, filter string) []Instance {
	filter = strings.ToLower(filter)
	var matched []Instance
	for _, inst := range instances {
		if strings.Contains(strings.ToLower(inst.Name), filter) {
			matched = append(matched, inst)
		}
	}
	return matched
}
