package config

import "testing"

func TestParseHostedDirectory(t *testing.T) {
	configuration, err := Parse([]string{
		"-listen", "127.0.0.1:8080",
		"-token", "12345678901234567890",
		"-hosted-dir", "  /srv/purpcmd/hosted  ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if configuration.HostedDir != "/srv/purpcmd/hosted" {
		t.Fatalf("hosted directory = %q", configuration.HostedDir)
	}
}

func TestParseRejectsEmptyHostedDirectory(t *testing.T) {
	_, err := Parse([]string{
		"-listen", "127.0.0.1:8080",
		"-token", "12345678901234567890",
		"-hosted-dir", "   ",
	})
	if err == nil {
		t.Fatal("empty hosted directory was accepted")
	}
}
