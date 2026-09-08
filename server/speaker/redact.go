package speaker

import (
	"net/http"
	"net/url"
	"sort"
	"strings"

	"purpcmd/pkg/teamapi"
)

const redactedValue = "[REDACTED]"

// Redact returns a detached API/event view with credential-bearing transport
// values removed. Runtime configuration and the encrypted-at-transport
// database representation are not modified.
func Redact(item teamapi.Speaker) teamapi.Speaker {
	result := cloneSpeaker(item)
	result.LastError = redactText(result.LastError, result.Config)
	result.Config.Client.Headers = redactHeader(result.Config.Client.Headers)
	result.Config.Client.Query = redactValues(result.Config.Client.Query)
	result.Config.Client.Cookies = redactStrings(result.Config.Client.Cookies)
	result.Config.Client.ProxyURL = redactString(result.Config.Client.ProxyURL)
	result.Config.Client.TLS.ClientKeyFile = redactString(result.Config.Client.TLS.ClientKeyFile)
	result.Config.Request.Headers = redactHeader(result.Config.Request.Headers)
	result.Config.Request.Query = redactValues(result.Config.Request.Query)
	result.Config.Request.Cookies = redactStrings(result.Config.Request.Cookies)
	return result
}

func redactText(text string, configuration teamapi.SpeakerConfig) string {
	values := make([]string, 0)
	appendHeaders := func(headers http.Header) {
		for _, entries := range headers {
			values = append(values, entries...)
		}
	}
	appendValues := func(entries url.Values) {
		for _, items := range entries {
			values = append(values, items...)
		}
	}
	appendStrings := func(entries map[string]string) {
		for _, value := range entries {
			values = append(values, value)
		}
	}
	appendHeaders(configuration.Client.Headers)
	appendValues(configuration.Client.Query)
	appendStrings(configuration.Client.Cookies)
	appendHeaders(configuration.Request.Headers)
	appendValues(configuration.Request.Query)
	appendStrings(configuration.Request.Cookies)
	values = append(values, configuration.Client.ProxyURL, configuration.Client.TLS.ClientKeyFile)

	// Replace longer values first so overlapping credentials cannot leave a
	// suffix behind. URL-encoded variants cover errors produced by net/http.
	sort.Slice(values, func(left, right int) bool { return len(values[left]) > len(values[right]) })
	for _, value := range values {
		if value == "" {
			continue
		}
		text = strings.ReplaceAll(text, value, redactedValue)
		text = strings.ReplaceAll(text, url.QueryEscape(value), redactedValue)
		text = strings.ReplaceAll(text, url.PathEscape(value), redactedValue)
	}
	return text
}

func RedactAll(items []teamapi.Speaker) []teamapi.Speaker {
	result := make([]teamapi.Speaker, len(items))
	for index, item := range items {
		result[index] = Redact(item)
	}
	return result
}

func redactHeader(values http.Header) http.Header {
	result := make(http.Header, len(values))
	for name, entries := range values {
		result[name] = make([]string, len(entries))
		for index := range entries {
			result[name][index] = redactedValue
		}
	}
	return result
}

func redactValues(values url.Values) url.Values {
	result := make(url.Values, len(values))
	for name, entries := range values {
		result[name] = make([]string, len(entries))
		for index := range entries {
			result[name][index] = redactedValue
		}
	}
	return result
}

func redactStrings(values map[string]string) map[string]string {
	result := make(map[string]string, len(values))
	for name := range values {
		result[name] = redactedValue
	}
	return result
}

func redactString(value string) string {
	if value == "" {
		return ""
	}
	return redactedValue
}
