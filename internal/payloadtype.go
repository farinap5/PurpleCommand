package internal

import "fmt"

const (
	DefaultPayloadType   = "impl"
	MaxPayloadTypeLength = 64
	MaxCommandNameLength = 64
)

// ValidatePayloadType validates the stable identifier an implant presents at
// registration and Lua uses to associate commands with that payload family.
func ValidatePayloadType(payloadType string) error {
	return validateIdentifier("payload type", payloadType, MaxPayloadTypeLength, true)
}

// ValidateCommandName validates a Lua command name as it appears in the CLI.
func ValidateCommandName(name string) error {
	return validateIdentifier("command name", name, MaxCommandNameLength, false)
}

func validateIdentifier(kind, value string, maximum int, allowDot bool) error {
	if value == "" {
		return fmt.Errorf("%s cannot be empty", kind)
	}
	if len(value) > maximum {
		return fmt.Errorf("%s exceeds %d bytes", kind, maximum)
	}
	for index, character := range []byte(value) {
		valid := character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' ||
			character == '_' || character == '-' || allowDot && character == '.'
		if !valid {
			return fmt.Errorf("%s contains invalid byte %q at offset %d", kind, character, index)
		}
	}
	return nil
}
