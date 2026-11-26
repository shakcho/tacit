package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/shakcho/tacit/internal/access"
	"github.com/shakcho/tacit/internal/platform/postgres"
)

var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr, os.Getenv); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) error {
	if len(args) == 1 && args[0] == "--version" {
		fmt.Fprintln(stdout, version)
		return nil
	}
	if len(args) == 1 && args[0] == "migrate" {
		if err := postgres.Migrate(ctx, getenv("TACIT_DATABASE_DIRECT_URL")); err != nil {
			return errors.New("database migration failed")
		}
		fmt.Fprintf(stdout, "database migrated to schema version %d\n", postgres.CurrentSchemaVersion)
		return nil
	}
	if len(args) == 1 && args[0] == "verify-schema" {
		if err := postgres.VerifySchema(ctx, getenv("TACIT_DATABASE_DIRECT_URL")); err != nil {
			return errors.New("database schema is incompatible or unavailable")
		}
		fmt.Fprintf(stdout, "database schema version %d verified\n", postgres.CurrentSchemaVersion)
		return nil
	}
	if len(args) > 0 && args[0] == "demo-bootstrap" {
		return runDemoBootstrap(ctx, args[1:], stdout, stderr, getenv)
	}
	if len(args) > 0 && args[0] == "invitation-grant" {
		return runInvitationGrant(ctx, args[1:], stdout, stderr, getenv)
	}
	return errors.New("usage: tacit-db migrate | tacit-db verify-schema | tacit-db demo-bootstrap [options] | tacit-db invitation-grant [options] | tacit-db --version")
}

func runDemoBootstrap(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) error {
	if getenv("TACIT_ENV") != "development" || getenv("TACIT_DEMO_BOOTSTRAP") != "1" {
		return errors.New("demo bootstrap requires TACIT_ENV=development and TACIT_DEMO_BOOTSTRAP=1")
	}
	databaseURL := getenv("TACIT_DATABASE_DIRECT_URL")
	if !loopbackDatabaseURL(databaseURL) {
		return errors.New("demo bootstrap requires a loopback PostgreSQL URL")
	}
	flags := flag.NewFlagSet("demo-bootstrap", flag.ContinueOnError)
	flags.SetOutput(stderr)
	namespaceValue := flags.String("namespace", "", "identity deployment namespace")
	externalUserValue := flags.String("external-user", "", "WorkOS user ID")
	externalOrganizationValue := flags.String("external-organization", "", "WorkOS organization ID")
	externalMembershipValue := flags.String("external-membership", "", "WorkOS membership ID")
	userValue := flags.String("user-id", "", "canonical local user ID")
	organizationValue := flags.String("organization-id", "", "canonical local organization ID")
	nameValue := flags.String("name", "Tacit demo", "local organization name")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return errors.New("invalid demo bootstrap options")
	}
	namespace, err := access.ParseIdentityNamespace(*namespaceValue)
	if err != nil {
		return errors.New("invalid demo identity namespace")
	}
	subject, err := access.ParseExternalSubject(*externalUserValue)
	if err != nil {
		return errors.New("invalid demo external user")
	}
	externalOrganization, err := access.ParseExternalOrganizationID(*externalOrganizationValue)
	if err != nil {
		return errors.New("invalid demo external organization")
	}
	externalMembership, err := access.ParseExternalMembershipID(*externalMembershipValue)
	if err != nil {
		return errors.New("invalid demo external membership")
	}
	user, err := access.ParseUserID(*userValue)
	if err != nil {
		return errors.New("invalid demo local user ID")
	}
	organization, err := access.ParseOrganizationID(*organizationValue)
	if err != nil {
		return errors.New("invalid demo local organization ID")
	}
	name, err := access.ParseName(*nameValue)
	if err != nil {
		return errors.New("invalid demo organization name")
	}
	if err := postgres.Migrate(ctx, databaseURL); err != nil {
		return errors.New("database migration failed")
	}
	store, err := postgres.Open(ctx, postgres.Config{
		URL: databaseURL, ApplicationName: "tacit-demo-bootstrap", MaxConnections: 1,
		ConnectTimeout: 5 * time.Second, HealthCheckPeriod: 30 * time.Second, PingTimeout: 5 * time.Second,
	})
	if err != nil {
		return errors.New("open demo database failed")
	}
	defer store.Close()
	if err := store.BootstrapIdentity(ctx, access.BootstrapIdentity{
		Namespace: namespace, Subject: subject, ExternalOrganization: externalOrganization,
		ExternalMembership: externalMembership, User: user, Organization: organization, OrganizationName: name,
	}); err != nil {
		return errors.New("demo identity bootstrap failed")
	}
	fmt.Fprintln(stdout, "demo identity bootstrap complete")
	return nil
}

func runInvitationGrant(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) error {
	if getenv("TACIT_ALLOW_INVITATION_GRANT") != "1" {
		return errors.New("invitation grant requires TACIT_ALLOW_INVITATION_GRANT=1")
	}
	databaseURL := getenv("TACIT_DATABASE_DIRECT_URL")
	if databaseURL == "" {
		return errors.New("invitation grant requires TACIT_DATABASE_DIRECT_URL")
	}
	flags := flag.NewFlagSet("invitation-grant", flag.ContinueOnError)
	flags.SetOutput(stderr)
	namespaceValue := flags.String("namespace", "", "identity deployment namespace")
	invitationValue := flags.String("external-invitation", "", "WorkOS invitation ID")
	organizationValue := flags.String("organization-id", "", "canonical local organization ID")
	roleValue := flags.String("role", "viewer", "local role: admin, editor, or viewer")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return errors.New("invalid invitation grant options")
	}
	namespace, err := access.ParseIdentityNamespace(*namespaceValue)
	if err != nil {
		return errors.New("invalid invitation identity namespace")
	}
	invitation, err := access.ParseExternalInvitationID(*invitationValue)
	if err != nil {
		return errors.New("invalid external invitation")
	}
	organization, err := access.ParseOrganizationID(*organizationValue)
	if err != nil {
		return errors.New("invalid invitation organization ID")
	}
	role, err := parseInvitationRole(*roleValue)
	if err != nil {
		return err
	}
	if err := postgres.Migrate(ctx, databaseURL); err != nil {
		return errors.New("database migration failed")
	}
	store, err := postgres.Open(ctx, postgres.Config{
		URL: databaseURL, ApplicationName: "tacit-invitation-grant", MaxConnections: 1,
		ConnectTimeout: 5 * time.Second, HealthCheckPeriod: 30 * time.Second, PingTimeout: 5 * time.Second,
	})
	if err != nil {
		return errors.New("open invitation database failed")
	}
	defer store.Close()
	if err := store.RegisterInvitationGrant(ctx, access.InvitationGrant{
		Namespace: namespace, Invitation: invitation, Organization: organization, Role: role,
	}); err != nil {
		return errors.New("register invitation grant failed")
	}
	fmt.Fprintln(stdout, "invitation grant registered")
	return nil
}

func parseInvitationRole(value string) (access.Role, error) {
	switch value {
	case "admin":
		return access.RoleAdmin, nil
	case "editor":
		return access.RoleEditor, nil
	case "viewer":
		return access.RoleViewer, nil
	default:
		return 0, errors.New("invalid invitation role")
	}
}

func loopbackDatabaseURL(value string) bool {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return false
	}
	hostname := parsed.Hostname()
	if hostname == "localhost" {
		return true
	}
	address := net.ParseIP(hostname)
	return address != nil && address.IsLoopback()
}
