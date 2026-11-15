package profiles

import (
	"testing"

	"github.com/yeixio/toskar-core/pkg/contracts"
)

func TestStockGeneralGainsInternet(t *testing.T) {
	stock := []contracts.ToolPolicy{
		{ToolID: "filesystem.read", Policy: "allow-for-session"},
		{ToolID: "filesystem.write", Policy: "ask"},
		{ToolID: "terminal", Policy: "ask"},
		{ToolID: "git.status", Policy: "allow"},
		{ToolID: "git.diff", Policy: "allow"},
		{ToolID: "git.add", Policy: "ask"},
		{ToolID: "git.commit", Policy: "ask"},
	}
	got := UpgradeGeneralTools(stock)
	if policy(got, "internet.search") != "allow" || policy(got, "internet.open") != "allow" {
		t.Fatalf("internet was not enabled: %#v", got)
	}
	if policy(got, "terminal") != "ask" || policy(got, "git.commit") != "ask" || policy(got, "filesystem.write") != "ask" {
		t.Fatalf("tools that change things should ask first: %#v", got)
	}
	if policy(got, "git.push") != "ask-each-time" {
		t.Fatalf("git.push should ask each time: %#v", got)
	}
}

func TestStockApprovalDefaultsAskFirst(t *testing.T) {
	stock := []contracts.ToolPolicy{
		{ToolID: "internet.search", Policy: "allow"},
		{ToolID: "filesystem.write", Policy: "ask"},
		{ToolID: "terminal", Policy: "ask"},
		{ToolID: "git.commit", Policy: "ask"},
	}
	got := UpgradeGeneralTools(stock)
	if policy(got, "terminal") != "ask" || policy(got, "filesystem.write") != "ask" || policy(got, "git.commit") != "ask" {
		t.Fatalf("stock ask policies changed: %#v", got)
	}
}

// Before #1206 the presets let writes, the terminal, and git run unasked. An
// untouched profile from then asks first now; a changed one keeps its choice.
func TestUnaskedPresetMovesToAsking(t *testing.T) {
	old := writesAllowed(generalToolPolicies())
	if policy(old, "git.push") != "allow" {
		t.Fatalf("snapshot is not the old preset: %#v", old)
	}
	got := UpgradeGeneralTools(old)
	if policy(got, "filesystem.write") != "ask" || policy(got, "git.push") != "ask-each-time" {
		t.Fatalf("untouched old preset kept allow: %#v", got)
	}
	changed := writesAllowed(generalToolPolicies())
	for i := range changed {
		if changed[i].ToolID == "internet.search" {
			changed[i].Policy = "deny"
		}
	}
	if got := UpgradeGeneralTools(changed); policy(got, "filesystem.write") != "allow" {
		t.Fatalf("a changed profile lost its choice: %#v", got)
	}
}

func TestCustomizedInternetOffIsPreserved(t *testing.T) {
	custom := []contracts.ToolPolicy{
		{ToolID: "internet.search", Policy: "deny"},
		{ToolID: "internet.open", Policy: "deny"},
		{ToolID: "filesystem.read", Policy: "allow"},
		{ToolID: "terminal", Policy: "deny"},
	}
	got := UpgradeGeneralTools(custom)
	if policy(got, "internet.search") != "deny" || policy(got, "internet.open") != "deny" {
		t.Fatalf("custom internet policy changed: %#v", got)
	}
	if policy(got, "terminal") != "deny" {
		t.Fatalf("custom terminal policy changed: %#v", got)
	}
}

func TestGeneralPresetExposesWebSearch(t *testing.T) {
	var general Profile
	for _, preset := range BuiltInPresets() {
		if preset.ID == PresetGeneral {
			general = preset
		}
	}
	if policy(general.Tools, "internet.search") != "allow" {
		t.Fatal("new General Assistant does not allow web search")
	}
}

func policy(tools []contracts.ToolPolicy, id string) string {
	for _, tool := range tools {
		if tool.ToolID == id {
			return tool.Policy
		}
	}
	return ""
}
