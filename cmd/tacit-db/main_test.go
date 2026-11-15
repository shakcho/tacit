package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/shakcho/tacit/internal/access"
)

func TestDemoBootstrapRequiresExplicitDevelopmentGuard(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{"demo-bootstrap"}, &stdout, &stderr, func(string) string { return "" })
	if err == nil || !strings.Contains(err.Error(), "TACIT_ENV=development") {
		t.Fatalf("run error = %v", err)
	}
}

func TestLoopbackDatabaseURL(t *testing.T) {
	tests := map[string]bool{
		"postgres://user:pass@127.0.0.1:5432/tacit":   true,
		"postgresql://user:pass@[::1]:5432/tacit":     true,
		"postgres://user:pass@localhost:5432/tacit":   true,
		"postgres://user:pass@database.example/tacit": false,
		"https://127.0.0.1/tacit":                     false,
		"":                                            false,
	}
	for value, want := range tests {
		if got := loopbackDatabaseURL(value); got != want {
			t.Errorf("loopbackDatabaseURL(%q) = %v, want %v", value, got, want)
		}
	}
}

func TestInvitationRoleIsExplicit(t *testing.T) {
	for value, want := range map[string]access.Role{
		"admin": access.RoleAdmin, "editor": access.RoleEditor, "viewer": access.RoleViewer,
	} {
		if got, err := parseInvitationRole(value); err != nil || got != want {
			t.Fatalf("parseInvitationRole(%q) = %v, %v", value, got, err)
		}
	}
	if _, err := parseInvitationRole("owner"); err == nil {
		t.Fatal("unknown invitation role succeeded")
	}
}

func TestInvitationGrantRequiresExplicitGuard(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{"invitation-grant"}, &stdout, &stderr, func(string) string { return "" })
	if err == nil || !strings.Contains(err.Error(), "TACIT_ALLOW_INVITATION_GRANT=1") {
		t.Fatalf("run error = %v", err)
	}
}
