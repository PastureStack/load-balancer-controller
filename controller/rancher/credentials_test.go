package rancher

import "testing"

func TestFirstCredentialPrefersRoleSpecificValue(t *testing.T) {
	const primary = "PASTURESTACK_TEST_PRIMARY_CREDENTIAL"
	const compatibility = "PASTURESTACK_TEST_COMPATIBILITY_CREDENTIAL"
	t.Setenv(primary, "role-specific")
	t.Setenv(compatibility, "compatibility")

	if actual := firstCredential(primary, compatibility); actual != "role-specific" {
		t.Fatalf("expected role-specific credential, got %q", actual)
	}
}

func TestFirstCredentialUsesCompatibilityValue(t *testing.T) {
	const primary = "PASTURESTACK_TEST_PRIMARY_CREDENTIAL"
	const compatibility = "PASTURESTACK_TEST_COMPATIBILITY_CREDENTIAL"
	t.Setenv(primary, "")
	t.Setenv(compatibility, "compatibility")

	if actual := firstCredential(primary, compatibility); actual != "compatibility" {
		t.Fatalf("expected compatibility credential, got %q", actual)
	}
}
