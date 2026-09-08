package implantbuilder

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"purpcmd/pkg/teamapi"
	"purpcmd/server/runtimeevents"
)

func TestRenderGoSourceSelectsBindRuntimeAndEmbedsOTS(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "template", "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	profile := Profile{
		Type: "bind.impl", Mode: "bind", LHOST: ":8443", PublicKey: filepath.Join("..", "..", "server.pub"),
		OTSToken: [12]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c},
	}
	rendered, err := RenderGoSource(profile, string(source))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		`payloadMode := "bind"`, `remoteAdd := ":8443"`, `payloadType := "bind.impl"`,
		`var implantOTS = [12]byte{0x01,0x02,0x03,0x04,0x05,0x06,0x07,0x08,0x09,0x0a,0x0b,0x0c}`,
		`core.StartBind(remoteAdd, payloadType)`, `var publicKeyDER = []byte{`,
	} {
		if !strings.Contains(rendered, expected) {
			t.Fatalf("rendered bind source is missing %q", expected)
		}
	}
	if strings.Contains(rendered, `"IMPLANT_MODE"`) || strings.Contains(rendered, `"IMPLANT_TYPE"`) || strings.Contains(rendered, `"LHOST"`) {
		t.Fatal("rendered bind source retained a build placeholder")
	}
}

func TestBundledMakefileSubstitutesBindRuntimeInputs(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("..", "..", "template", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	makefile := string(contents)
	for _, expected := range []string{
		`MODE      ?= reverse`,
		`OTS_BYTES ?=`,
		`'s|"IMPLANT_MODE"|"$(MODE)"|g'`,
		`'s|var implantOTS \[12\]byte|var implantOTS = [12]byte{$(OTS_BYTES)}|g'`,
	} {
		if !strings.Contains(makefile, expected) {
			t.Fatalf("bundled Makefile is missing %q", expected)
		}
	}
}

func TestRegisteredPayloadBuilderDispatchAndCleanup(t *testing.T) {
	const source = "payloadbuilder-test.lua"
	UnregisterPayloadBuilders(source)
	t.Cleanup(func() { UnregisterPayloadBuilders(source) })

	templateDirectory := t.TempDir()
	output := filepath.Join(t.TempDir(), "payload")
	called := false
	if err := RegisterPayloadBuilder("test-linux-amd64", "test builder", source, func(name string, profile Profile) error {
		called = true
		if name != "profile-one" {
			return errors.New("unexpected profile name")
		}
		if !filepath.IsAbs(profile.Template) || !filepath.IsAbs(profile.Output) {
			return errors.New("builder paths are not absolute")
		}
		return os.WriteFile(profile.Output, []byte("artifact"), 0600)
	}); err != nil {
		t.Fatal(err)
	}
	if err := RegisterPayloadBuilder("test-linux-amd64", "duplicate", "other.lua", func(string, Profile) error { return nil }); err == nil {
		t.Fatal("duplicate payload builder registration was accepted")
	}

	profile := &Profile{
		Type: "impl", LHOST: "127.0.0.1:4444", OS: "linux", ARCH: "amd64",
		Template: templateDirectory, Output: output, Builder: "test-linux-amd64",
	}
	if err := generate("profile-one", profile); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("registered payload builder was not called")
	}
	if content, err := os.ReadFile(output); err != nil || string(content) != "artifact" {
		t.Fatalf("payload artifact = %q, %v", content, err)
	}
	descriptions := PayloadBuilderDescriptions()
	if len(descriptions) != 1 || descriptions[0][0] != "test-linux-amd64" || descriptions[0][1] != "test builder" {
		t.Fatalf("payload builder descriptions = %#v", descriptions)
	}
	builders := APIListPayloadBuilders()
	if len(builders) != 1 || builders[0].Name != "test-linux-amd64" ||
		builders[0].Description != "test builder" || builders[0].Source != source {
		t.Fatalf("payload builders = %#v", builders)
	}

	UnregisterPayloadBuilders(source)
	if err := generate("profile-one", profile); err == nil || !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("missing payload builder error = %v", err)
	}
}

func TestRegisteredPayloadBuilderRequiresArtifact(t *testing.T) {
	const source = "payloadbuilder-no-artifact.lua"
	UnregisterPayloadBuilders(source)
	t.Cleanup(func() { UnregisterPayloadBuilders(source) })
	if err := RegisterPayloadBuilder("no-artifact", "", source, func(string, Profile) error { return nil }); err != nil {
		t.Fatal(err)
	}
	profile := &Profile{
		Type: "impl", LHOST: "127.0.0.1:4444", OS: "linux", ARCH: "amd64",
		Template: t.TempDir(), Output: filepath.Join(t.TempDir(), "missing"), Builder: "no-artifact",
	}
	if err := generate("missing", profile); err == nil || !strings.Contains(err.Error(), "did not produce") {
		t.Fatalf("missing artifact error = %v", err)
	}
}

func TestPayloadBuilderRegistrationPublishesLifecycleEvents(t *testing.T) {
	const source = "payloadbuilder-lifecycle.lua"
	UnregisterPayloadBuilders(source)
	runtimeevents.SetPublisher(nil)
	t.Cleanup(func() {
		runtimeevents.SetPublisher(nil)
		UnregisterPayloadBuilders(source)
	})

	types := make([]string, 0, 2)
	values := make([]teamapi.PayloadBuilder, 0, 2)
	runtimeevents.SetPublisher(func(eventType string, value any) {
		builder, ok := value.(teamapi.PayloadBuilder)
		if !ok {
			t.Fatalf("payload builder event value = %T", value)
		}
		types = append(types, eventType)
		values = append(values, builder)
	})

	if err := RegisterPayloadBuilder("lifecycle-builder", "lifecycle", source, func(string, Profile) error { return nil }); err != nil {
		t.Fatal(err)
	}
	UnregisterPayloadBuilders(source)
	if len(types) != 2 || types[0] != teamapi.EventPayloadBuilderRegistered ||
		types[1] != teamapi.EventPayloadBuilderUnregistered {
		t.Fatalf("payload builder event types = %#v", types)
	}
	for _, builder := range values {
		if builder.Name != "lifecycle-builder" || builder.Description != "lifecycle" || builder.Source != source {
			t.Fatalf("payload builder event = %#v", builder)
		}
	}
}

func TestBuildCommandPublishesCorrelatedOutput(t *testing.T) {
	runtimeevents.SetPublisher(nil)
	t.Cleanup(func() { runtimeevents.SetPublisher(nil) })
	var eventType string
	var output teamapi.BuildOutput
	runtimeevents.SetPublisher(func(gotType string, value any) {
		eventType = gotType
		var ok bool
		output, ok = value.(teamapi.BuildOutput)
		if !ok {
			t.Fatalf("build output event value = %T", value)
		}
	})
	profile := Profile{ProfileName: "linux-impl", BuildID: "build-123"}
	if err := runBuildCommand(exec.Command("/bin/sh", "-c", "printf 'compiler output'"), profile, "go"); err != nil {
		t.Fatal(err)
	}
	if eventType != teamapi.EventBuildOutput || output.BuildID != "build-123" ||
		output.Profile != "linux-impl" || output.Builder != "go" || output.Message != "compiler output" {
		t.Fatalf("build output event = %q %#v", eventType, output)
	}
}

func TestBuildOutputCaptureIsBounded(t *testing.T) {
	var capture BuildOutputCapture
	input := strings.Repeat("x", MaxBuildOutputBytes+1024)
	written, err := capture.Write([]byte(input))
	if err != nil || written != len(input) {
		t.Fatalf("capture write = %d, %v", written, err)
	}
	output := capture.String()
	if !strings.HasSuffix(output, "\n[build output truncated]\n") || len(output) > MaxBuildOutputBytes+64 {
		t.Fatalf("bounded output length/suffix = %d, %q", len(output), output[len(output)-32:])
	}
}
