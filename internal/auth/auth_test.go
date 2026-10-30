package auth

import "testing"

func TestRequireRole(t *testing.T) {
	if err := RequireRole(RoleRider, ""); err == nil {
		t.Fatal("RequireRole() expected missing token error")
	}
	if err := RequireRole(RoleRider, "rider"); err != nil {
		t.Fatalf("RequireRole() returned error: %v", err)
	}
	if err := RequireRole(RoleAdmin, "driver"); err == nil {
		t.Fatal("RequireRole() expected role mismatch error")
	}
}
