package internal

import "testing"

func TestValidatePayloadType(t *testing.T) {
	valid := []string{"impl", "linux.impl", "windows-x64", "iot_v2", "Type1"}
	for _, payloadType := range valid {
		if err := ValidatePayloadType(payloadType); err != nil {
			t.Fatalf("valid payload type %q: %v", payloadType, err)
		}
	}

	invalid := []string{"", "two words", " leading", "type/name", "type:one"}
	for _, payloadType := range invalid {
		if err := ValidatePayloadType(payloadType); err == nil {
			t.Fatalf("accepted invalid payload type %q", payloadType)
		}
	}
}

func TestValidateCommandName(t *testing.T) {
	for _, name := range []string{"ping", "file-upload", "cmd_2"} {
		if err := ValidateCommandName(name); err != nil {
			t.Fatalf("valid command name %q: %v", name, err)
		}
	}
	for _, name := range []string{"", "two words", "type.command"} {
		if err := ValidateCommandName(name); err == nil {
			t.Fatalf("accepted invalid command name %q", name)
		}
	}
}
