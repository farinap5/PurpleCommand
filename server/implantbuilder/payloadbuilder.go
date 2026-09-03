package implantbuilder

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"purpcmd/internal"
	"purpcmd/pkg/teamapi"
	"purpcmd/server/log"
	"purpcmd/server/runtimeevents"
)

// PayloadBuildFunc is the integration boundary for externally registered
// builders such as Lua scripts. The profile is a defensive copy whose output,
// template, and public-key paths are absolute.
type PayloadBuildFunc func(profileName string, profile Profile) error

type payloadBuilderRegistration struct {
	Name        string
	Description string
	Source      string
	Build       PayloadBuildFunc
}

var (
	payloadBuildersMu sync.RWMutex
	payloadBuilders   = make(map[string]payloadBuilderRegistration)
)

func RegisterPayloadBuilder(name, description, source string, build PayloadBuildFunc) error {
	name = strings.TrimSpace(name)
	source = strings.TrimSpace(source)
	if err := internal.ValidateCommandName(name); err != nil {
		return fmt.Errorf("payload builder: %w", err)
	}
	if source == "" {
		return errors.New("payload builder source is required")
	}
	if build == nil {
		return errors.New("payload builder function is required")
	}
	payloadBuildersMu.Lock()
	if existing, found := payloadBuilders[name]; found {
		payloadBuildersMu.Unlock()
		return fmt.Errorf("payload builder %q is already registered by %s", name, existing.Source)
	}
	registration := payloadBuilderRegistration{
		Name: name, Description: strings.TrimSpace(description), Source: source, Build: build,
	}
	payloadBuilders[name] = registration
	payloadBuildersMu.Unlock()
	runtimeevents.Publish(teamapi.EventPayloadBuilderRegistered, payloadBuilderDTO(registration))
	return nil
}

func UnregisterPayloadBuilders(source string) {
	payloadBuildersMu.Lock()
	removed := make([]payloadBuilderRegistration, 0)
	for name, builder := range payloadBuilders {
		if builder.Source == source {
			removed = append(removed, builder)
			delete(payloadBuilders, name)
		}
	}
	payloadBuildersMu.Unlock()
	sort.Slice(removed, func(left, right int) bool { return removed[left].Name < removed[right].Name })
	for _, builder := range removed {
		runtimeevents.Publish(teamapi.EventPayloadBuilderUnregistered, payloadBuilderDTO(builder))
	}
}

func PayloadBuilderDescriptions() [][]string {
	builders := APIListPayloadBuilders()
	result := make([][]string, 0, len(builders))
	for _, builder := range builders {
		result = append(result, []string{builder.Name, builder.Description})
	}
	return result
}

// APIListPayloadBuilders returns a sorted defensive snapshot of all builders
// registered by currently loaded Lua scripts.
func APIListPayloadBuilders() []teamapi.PayloadBuilder {
	payloadBuildersMu.RLock()
	result := make([]teamapi.PayloadBuilder, 0, len(payloadBuilders))
	for _, builder := range payloadBuilders {
		result = append(result, payloadBuilderDTO(builder))
	}
	payloadBuildersMu.RUnlock()
	sort.Slice(result, func(left, right int) bool { return result[left].Name < result[right].Name })
	return result
}

// ValidatePayloadBuilder verifies that a requested builder name is valid and
// currently registered. An empty name is reserved for the legacy build path.
func ValidatePayloadBuilder(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	if err := internal.ValidateCommandName(name); err != nil {
		return fmt.Errorf("payload builder: %w", err)
	}
	payloadBuildersMu.RLock()
	_, found := payloadBuilders[name]
	payloadBuildersMu.RUnlock()
	if !found {
		return fmt.Errorf("payload builder %q is not registered", name)
	}
	return nil
}

func payloadBuilderDTO(builder payloadBuilderRegistration) teamapi.PayloadBuilder {
	return teamapi.PayloadBuilder{
		Name: builder.Name, Description: builder.Description, Source: builder.Source,
	}
}

func generateWithPayloadBuilder(profileName string, profile *Profile) error {
	payloadBuildersMu.RLock()
	builder, found := payloadBuilders[profile.Builder]
	payloadBuildersMu.RUnlock()
	if !found {
		return fmt.Errorf("payload builder %q is not registered", profile.Builder)
	}

	log.PrintInfo(fmt.Sprintf("[%s] Building with Lua payload builder %s", profileName, builder.Name))
	if err := builder.Build(profileName, cloneProfile(*profile)); err != nil {
		return fmt.Errorf("payload builder %q failed: %w", builder.Name, err)
	}
	info, err := os.Stat(profile.Output)
	if err != nil {
		return fmt.Errorf("payload builder %q did not produce %s: %w", builder.Name, profile.Output, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("payload builder %q output %s is not a regular file", builder.Name, profile.Output)
	}
	log.PrintSuccs(fmt.Sprintf("[%s] Implant written to: %s", profileName, profile.Output))
	return nil
}
