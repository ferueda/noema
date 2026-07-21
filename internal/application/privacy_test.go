package application

import (
	"encoding/base64"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestPrivacyPolicyPreflightBlocksSecretsWithoutReturningTheirValues(t *testing.T) {
	jwt := testJWT()
	secret := "ghp_" + strings.Repeat("a", 24)
	values := []string{
		"-----BEGIN OPENSSH PRIVATE KEY-----",
		"-----BEGIN PGP PRIVATE KEY BLOCK-----",
		`{"Authorization":"Bearer hidden-value"}`,
		`{\"Authorization\":\"Bearer escaped-hidden-value\"}`,
		"{\"Authorization\":\n \"Bearer multiline-hidden-value\"}",
		"Proxy-Authorization: Basic another-hidden-value",
		"token=" + secret,
		"session=" + jwt,
	}

	sanitized, report, err := (PrivacyPolicy{}).PreflightBatch(values)
	if sanitized != nil {
		t.Fatalf("sanitized values = %#v, want nil", sanitized)
	}
	var violation PrivacyViolation
	if !errors.As(err, &violation) {
		t.Fatalf("error = %v, want PrivacyViolation", err)
	}
	want := []string{privacyAuthorization, privacyJWT, privacyPrivateKey, privacyProviderToken}
	if !reflect.DeepEqual(report.BlockedCategories, want) || !reflect.DeepEqual(violation.Categories, want) {
		t.Fatalf("blocked categories = %#v / %#v, want %#v", report.BlockedCategories, violation.Categories, want)
	}
	if report.PolicyVersion != PrivacyPolicyVersion || len(report.Redactions) != 0 {
		t.Fatalf("report = %#v", report)
	}
	for _, protected := range []string{secret, jwt, "hidden-value", "OPENSSH"} {
		if strings.Contains(err.Error(), protected) {
			t.Fatalf("error exposed protected value %q: %v", protected, err)
		}
	}
}

func TestPrivacyPolicyDoesNotTrustCredentialPlaceholderTextFromInput(t *testing.T) {
	value := "https://" + privacyURLCredentialsPlaceholder + ":real-secret@example.com/private"

	sanitized, report, err := (PrivacyPolicy{}).Preflight(value)
	if err == nil || sanitized != "" || !reflect.DeepEqual(report.BlockedCategories, []string{privacyURLCredentials}) {
		t.Fatalf("preflight = %q / %#v, %v", sanitized, report, err)
	}

	postflight, err := (PrivacyPolicy{}).Postflight(value)
	if err == nil || !reflect.DeepEqual(postflight.BlockedCategories, []string{privacyURLCredentials}) {
		t.Fatalf("postflight = %#v, %v", postflight, err)
	}
}

func TestPrivacyPolicyPreflightRedactsSupportedLocalDetails(t *testing.T) {
	values := []string{
		"edit /Users/example/dev/project/main.go and C:\\Users\\example\\dev\\main.go",
		"fetch https://reader:password@example.com/resource",
		"connect http://10.0.0.4:8080/api, localhost:3000, and https://example.com/public",
	}

	sanitized, report, err := (PrivacyPolicy{}).PreflightBatch(values)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if strings.Contains(strings.Join(sanitized, "\n"), "example/dev") ||
		strings.Contains(strings.Join(sanitized, "\n"), "reader:password") ||
		strings.Contains(strings.Join(sanitized, "\n"), "10.0.0.4") ||
		strings.Contains(strings.Join(sanitized, "\n"), "localhost") {
		t.Fatalf("sanitized values retain protected details: %#v", sanitized)
	}
	if !strings.Contains(sanitized[0], privacyLocalPathPlaceholder) ||
		!strings.Contains(sanitized[1], "https://"+privacyURLCredentialsPlaceholder+"@example.com/resource") ||
		!strings.Contains(sanitized[2], "http://"+privacyPrivateHostPlaceholder+"/api") ||
		!strings.Contains(sanitized[2], "https://example.com/public") {
		t.Fatalf("sanitized values = %#v", sanitized)
	}
	want := []PrivacyCategoryCount{
		{Category: privacyLocalOrPrivateHost, Count: 2},
		{Category: privacyLocalPath, Count: 2},
		{Category: privacyURLCredentials, Count: 1},
	}
	if !reflect.DeepEqual(report.Redactions, want) || len(report.BlockedCategories) != 0 {
		t.Fatalf("report = %#v, want redactions %#v", report, want)
	}

	again, secondReport, err := (PrivacyPolicy{}).PreflightBatch(sanitized)
	if err != nil {
		t.Fatalf("second preflight: %v", err)
	}
	if !reflect.DeepEqual(again, sanitized) || len(secondReport.Redactions) != 0 {
		t.Fatalf("second preflight = %#v / %#v", again, secondReport)
	}
}

func TestPrivacyPolicyPostflightRejectsInsteadOfRedacting(t *testing.T) {
	values := []string{
		"safe statement",
		"scope /home/example/private/project",
		"subject https://user:pass@example.com/private",
		"host 192.168.1.8",
	}
	report, err := (PrivacyPolicy{}).Postflight(values...)
	var violation PrivacyViolation
	if !errors.As(err, &violation) {
		t.Fatalf("error = %v, want PrivacyViolation", err)
	}
	want := []string{privacyLocalOrPrivateHost, privacyLocalPath, privacyURLCredentials}
	if !reflect.DeepEqual(report.BlockedCategories, want) || len(report.Redactions) != 0 {
		t.Fatalf("report = %#v, want blocked %#v", report, want)
	}

	safe, err := (PrivacyPolicy{}).Postflight("relative/path.go", "https://example.com/public", "release.1.2")
	if err != nil || len(safe.BlockedCategories) != 0 {
		t.Fatalf("safe postflight = %#v, %v", safe, err)
	}
}

func TestPrivacyPolicyRequiresValidatedJWTStructure(t *testing.T) {
	for _, value := range []string{
		"release.2026.07",
		"abcd.efgh.ijkl",
		base64.RawURLEncoding.EncodeToString([]byte(`{"typ":"JWT"}`)) + "." +
			base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"example"}`)) + ".signature",
	} {
		if _, _, err := (PrivacyPolicy{}).Preflight(value); err != nil {
			t.Fatalf("preflight %q: %v", value, err)
		}
	}

	jwt := testJWT()
	_, report, err := (PrivacyPolicy{}).Preflight(jwt)
	if err == nil || !reflect.DeepEqual(report.BlockedCategories, []string{privacyJWT}) {
		t.Fatalf("JWT preflight = %#v, %v", report, err)
	}

	unsigned := unsignedTestJWT()
	_, report, err = (PrivacyPolicy{}).Preflight(unsigned)
	if err == nil || !reflect.DeepEqual(report.BlockedCategories, []string{privacyJWT}) {
		t.Fatalf("unsigned JWT preflight = %#v, %v", report, err)
	}
}

func TestPrivacyPolicyRedactsArbitraryAbsolutePathsAndFileURLs(t *testing.T) {
	values := []string{
		"read /opt/private/repo/config.json",
		"inspect /workspace/project/main.go",
		"compare /etc/internal/config",
		"open /root/private/notes.txt",
		"stage /private/tmp/noema/input.json",
		"load file:///var/lib/noema/private.db",
	}

	sanitized, report, err := (PrivacyPolicy{}).PreflightBatch(values)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	for index, value := range sanitized {
		if value != strings.Fields(values[index])[0]+" "+privacyLocalPathPlaceholder {
			t.Fatalf("sanitized[%d] = %q", index, value)
		}
	}
	want := []PrivacyCategoryCount{{Category: privacyLocalPath, Count: len(values)}}
	if !reflect.DeepEqual(report.Redactions, want) {
		t.Fatalf("redactions = %#v, want %#v", report.Redactions, want)
	}
}

func TestPrivacyPolicyRedactsCompleteQuotedPaths(t *testing.T) {
	values := []string{
		"open `/Users/example/My Private Project/main.go`",
		`{"path":"/workspace/My Private Project/config.json"}`,
		`inspect 'D:\work\Private Project\main.go'`,
	}
	want := []string{
		"open `" + privacyLocalPathPlaceholder + "`",
		`{"path":"` + privacyLocalPathPlaceholder + `"}`,
		"inspect '" + privacyLocalPathPlaceholder + "'",
	}

	sanitized, report, err := (PrivacyPolicy{}).PreflightBatch(values)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if !reflect.DeepEqual(sanitized, want) {
		t.Fatalf("sanitized = %#v, want %#v", sanitized, want)
	}
	if !reflect.DeepEqual(report.Redactions, []PrivacyCategoryCount{{Category: privacyLocalPath, Count: len(values)}}) {
		t.Fatalf("redactions = %#v", report.Redactions)
	}
}

func TestPrivacyPolicyRedactsPrivateAddressFamiliesAndLeavesPublicAddresses(t *testing.T) {
	value := "127.0.0.1 172.16.4.2 192.168.2.3 169.254.1.2 100.64.0.1 100.127.255.254 [::1] [fe80::1] fc00::1 fe80::2 8.8.8.8 100.128.0.1 [2606:4700:4700::1111]"
	sanitized, report, err := (PrivacyPolicy{}).Preflight(value)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if strings.Contains(sanitized, "127.0.0.1") || strings.Contains(sanitized, "fe80::1") ||
		strings.Contains(sanitized, "fc00::1") || strings.Contains(sanitized, "fe80::2") ||
		strings.Contains(sanitized, "100.64.0.1") || strings.Contains(sanitized, "100.127.255.254") ||
		!strings.Contains(sanitized, "8.8.8.8") || !strings.Contains(sanitized, "100.128.0.1") ||
		!strings.Contains(sanitized, "2606:4700:4700::1111") {
		t.Fatalf("sanitized value = %q", sanitized)
	}
	want := []PrivacyCategoryCount{{Category: privacyLocalOrPrivateHost, Count: 10}}
	if !reflect.DeepEqual(report.Redactions, want) {
		t.Fatalf("redactions = %#v, want %#v", report.Redactions, want)
	}
}

func testJWT() string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"example"}`))
	return header + "." + payload + ".signature-value"
}

func unsignedTestJWT() string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"private-example"}`))
	return header + "." + payload + "."
}
