package listener

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"unicode"
)

// HTTPProfileLHOST returns the advertised callback authority that the current
// Lua-built implant can consume. The existing client supports only persistent,
// plain-HTTP listeners using the driver's default callback routes.
func HTTPProfileLHOST(snapshot ManagedListenerSnapshot) (string, error) {
	if normalizeRegistryID(snapshot.Config.Driver) != "http" {
		return "", fmt.Errorf("listener driver %q is not compatible with an HTTP implant profile", snapshot.Config.Driver)
	}
	if !snapshot.Config.Persistent {
		return "", errors.New("only persistent listeners can be attached to an implant profile")
	}
	if len(snapshot.Config.Routes) != 0 {
		return "", errors.New("the current implant supports only the HTTP listener default routes")
	}
	options, err := resolveHTTPOptions(snapshot.Config.Options)
	if err != nil {
		return "", err
	}
	if options.TLS.Enabled {
		return "", errors.New("the current implant does not support HTTPS listener attachments")
	}
	host, err := validProfileHost(options.Advertise.Host)
	if err != nil {
		return "", fmt.Errorf("HTTP listener advertise host: %w", err)
	}
	port, err := strconv.Atoi(options.Advertise.Port)
	if err != nil || port < 1 || port > 65535 {
		return "", errors.New("HTTP listener advertise port must be a number between 1 and 65535")
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}

func validProfileHost(value string) (string, error) {
	host := strings.TrimSpace(value)
	if host == "" {
		return "", errors.New("is required")
	}
	bracketed := strings.HasPrefix(host, "[") || strings.HasSuffix(host, "]")
	if bracketed {
		if !strings.HasPrefix(host, "[") || !strings.HasSuffix(host, "]") {
			return "", errors.New("has unmatched IPv6 brackets")
		}
		host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	}
	if host == "" || strings.ContainsAny(host, "[]") {
		return "", errors.New("is malformed")
	}
	for _, character := range host {
		if unicode.IsSpace(character) || unicode.IsControl(character) {
			return "", errors.New("must not contain whitespace or control characters")
		}
	}
	if strings.ContainsAny(host, "/\\?#@") {
		return "", errors.New("must be a host without a URL scheme, path, query, or user information")
	}
	if strings.Count(host, "%") > 1 {
		return "", errors.New("has an invalid IPv6 zone")
	}
	if zoneAt := strings.IndexByte(host, '%'); zoneAt >= 0 {
		zone := host[zoneAt+1:]
		if zone == "" {
			return "", errors.New("has an empty IPv6 zone")
		}
		for _, character := range zone {
			if !isProfileZoneCharacter(character) {
				return "", errors.New("has an invalid IPv6 zone")
			}
		}
	}
	if address, err := netip.ParseAddr(host); err == nil {
		if bracketed && !address.Is6() {
			return "", errors.New("brackets are valid only for IPv6 addresses")
		}
		connectAddress := address.WithZone("").Unmap()
		if connectAddress.IsUnspecified() {
			return "", errors.New("must be a connectable address, not a wildcard")
		}
		if address.Zone() != "" {
			host = address.WithZone("").String() + "%25" + address.Zone()
		}
		return host, nil
	}
	if bracketed || strings.ContainsAny(host, ":%") {
		return "", errors.New("is not a valid IPv6 address or hostname")
	}
	if err := validateProfileHostname(host); err != nil {
		return "", err
	}
	return host, nil
}

func isProfileZoneCharacter(character rune) bool {
	return character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
		character >= '0' && character <= '9' || character == '_' || character == '-' || character == '.'
}

func validateProfileHostname(host string) error {
	if len(host) > 253 {
		return errors.New("hostname is longer than 253 characters")
	}
	trimmed := strings.TrimSuffix(host, ".")
	if trimmed == "" {
		return errors.New("hostname is empty")
	}
	for _, label := range strings.Split(trimmed, ".") {
		if len(label) == 0 || len(label) > 63 {
			return errors.New("hostname contains an empty or oversized label")
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return errors.New("hostname labels must not start or end with a hyphen")
		}
		for _, character := range label {
			if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
				character >= '0' && character <= '9' || character == '-' {
				continue
			}
			return errors.New("hostname contains an invalid character")
		}
	}
	return nil
}
