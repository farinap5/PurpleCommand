package cli

import "testing"

func TestShouldShowBuildOutputOnlyForRelevantImplantProfile(t *testing.T) {
	cli := &CLI{mode: modeProfile, selectedProfile: "linux"}
	if !cli.shouldShowBuildOutput("linux") {
		t.Fatal("selected profile build output was hidden")
	}
	if cli.shouldShowBuildOutput("windows") {
		t.Fatal("unrelated profile build output was shown")
	}
	cli.selectedProfile = ""
	if !cli.shouldShowBuildOutput("windows") {
		t.Fatal("build output was hidden with no selected profile")
	}
	cli.mode = modeSession
	if cli.shouldShowBuildOutput("windows") {
		t.Fatal("build output was shown outside implant mode")
	}
}
