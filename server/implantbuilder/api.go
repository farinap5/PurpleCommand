package implantbuilder

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"purpcmd/internal"
	"purpcmd/pkg/teamapi"
	"purpcmd/server/db"
)

var profileAPIMu sync.Mutex

func APIListProfiles() []teamapi.Profile {
	profileAPIMu.Lock()
	defer profileAPIMu.Unlock()
	result := make([]teamapi.Profile, 0, len(ProfileMap))
	for name, profile := range ProfileMap {
		result = append(result, profileDTO(name, profile))
	}
	return result
}

func APIGetProfile(name string) (teamapi.Profile, error) {
	profileAPIMu.Lock()
	defer profileAPIMu.Unlock()
	profile := ProfileMap[name]
	if profile == nil {
		return teamapi.Profile{}, errors.New("profile not found")
	}
	return profileDTO(name, profile), nil
}

func APICreateProfile(request teamapi.Profile) (teamapi.Profile, error) {
	profileAPIMu.Lock()
	defer profileAPIMu.Unlock()
	request.Name = strings.TrimSpace(request.Name)
	if request.Name == "" {
		return teamapi.Profile{}, errors.New("profile name is required")
	}
	if ProfileMap[request.Name] != nil {
		return teamapi.Profile{}, errors.New("profile already exists")
	}
	profile := defaultProfile()
	applyProfileDTO(profile, request)
	if err := validateProfile(profile); err != nil {
		return teamapi.Profile{}, err
	}
	if err := db.DBImplantProfileInsert(profileToDBRow(request.Name, profile)); err != nil {
		return teamapi.Profile{}, err
	}
	if profileRequestHasDefinition(request) {
		definition, err := definitionFromProfileRequest(request.Name, profile, request)
		if err != nil {
			_ = db.DBImplantProfileDelete(request.Name)
			return teamapi.Profile{}, err
		}
		if err := db.DBImplantDefinitionUpsert(definition); err != nil {
			_ = db.DBImplantProfileDelete(request.Name)
			return teamapi.Profile{}, err
		}
	}
	ProfileMap[request.Name] = profile
	return profileDTO(request.Name, profile), nil
}

func APIUpdateProfile(request teamapi.ProfileUpdateRequest) (teamapi.Profile, error) {
	profileAPIMu.Lock()
	defer profileAPIMu.Unlock()
	profile := ProfileMap[request.Name]
	if profile == nil {
		return teamapi.Profile{}, errors.New("profile not found")
	}
	key := strings.ToUpper(strings.TrimSpace(request.Key))
	if isDefinitionUpdateKey(key) {
		if err := updateProfileDefinition(request.Name, profile, key, request.Value); err != nil {
			return teamapi.Profile{}, err
		}
		return profileDTO(request.Name, profile), nil
	}

	updated := cloneProfile(*profile)
	switch key {
	case "TYPE":
		updated.Type = request.Value
	case "LHOST":
		updated.LHOST = request.Value
		updated.ListenerUUID = ""
	case "OS":
		updated.OS = request.Value
		updated.OSOptions = appendSuggestion(updated.OSOptions, request.Value)
	case "ARCH":
		updated.ARCH = request.Value
		updated.ARCHOptions = appendSuggestion(updated.ARCHOptions, request.Value)
	case "OUTPUT":
		updated.Output = request.Value
	case "TEMPLATE":
		updated.Template = request.Value
	case "PUBLICKEY":
		updated.PublicKey = request.Value
	case "BUILDER":
		updated.Builder = request.Value
	default:
		return teamapi.Profile{}, fmt.Errorf("unknown option %q", request.Key)
	}
	if err := validateProfile(&updated); err != nil {
		return teamapi.Profile{}, err
	}
	if err := db.DBImplantProfileUpdate(profileToDBRow(request.Name, &updated)); err != nil {
		return teamapi.Profile{}, err
	}
	*profile = updated
	return profileDTO(request.Name, profile), nil
}

func APIDeleteProfile(name string) error {
	profileAPIMu.Lock()
	defer profileAPIMu.Unlock()
	if ProfileMap[name] == nil {
		return errors.New("profile not found")
	}
	if err := db.DBImplantProfileDelete(name); err != nil {
		return err
	}
	delete(ProfileMap, name)
	if CurrentName == name {
		CurrentName = ""
	}
	return nil
}

// APISetProfileListener atomically attaches, refreshes, or detaches the HTTP
// listener associated with a profile. Detaching uses an empty listener UUID
// and deliberately preserves the last materialized LHOST.
func APISetProfileListener(name, listenerUUID, lhost string) (teamapi.Profile, bool, error) {
	profileAPIMu.Lock()
	defer profileAPIMu.Unlock()
	name = strings.TrimSpace(name)
	profile := ProfileMap[name]
	if profile == nil {
		return teamapi.Profile{}, false, errors.New("profile not found")
	}
	listenerUUID = strings.TrimSpace(listenerUUID)
	if listenerUUID != "" {
		lhost = strings.TrimSpace(lhost)
		if lhost == "" {
			return teamapi.Profile{}, false, errors.New("listener advertisement is required")
		}
		definition, err := db.DBImplantDefinitionGet(name)
		if errors.Is(err, sql.ErrNoRows) || err == nil && !strings.EqualFold(strings.TrimSpace(definition.Protocol), "http") {
			return teamapi.Profile{}, false, errors.New("listener attachments require an HTTP implant profile")
		}
		if err != nil {
			return teamapi.Profile{}, false, err
		}
	}
	if profile.ListenerUUID == listenerUUID && (listenerUUID == "" || profile.LHOST == lhost) {
		return profileDTO(name, profile), false, nil
	}
	updated := cloneProfile(*profile)
	updated.ListenerUUID = listenerUUID
	if listenerUUID != "" {
		updated.LHOST = lhost
	}
	if err := validateProfile(&updated); err != nil {
		return teamapi.Profile{}, false, err
	}
	if err := db.DBImplantProfileUpdate(profileToDBRow(name, &updated)); err != nil {
		return teamapi.Profile{}, false, err
	}
	*profile = updated
	return profileDTO(name, profile), true, nil
}

// APIClearProfileListeners removes all associations to a deleted listener and
// preserves each profile's last materialized LHOST.
func APIClearProfileListeners(listenerUUID string) ([]teamapi.Profile, error) {
	profileAPIMu.Lock()
	defer profileAPIMu.Unlock()
	listenerUUID = strings.TrimSpace(listenerUUID)
	if listenerUUID == "" {
		return []teamapi.Profile{}, nil
	}
	names := make([]string, 0)
	for name, profile := range ProfileMap {
		if profile.ListenerUUID == listenerUUID {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return []teamapi.Profile{}, nil
	}
	if _, err := db.DBImplantProfilesClearListener(listenerUUID); err != nil {
		return nil, err
	}
	sort.Strings(names)
	result := make([]teamapi.Profile, 0, len(names))
	for _, name := range names {
		ProfileMap[name].ListenerUUID = ""
		result = append(result, profileDTO(name, ProfileMap[name]))
	}
	return result, nil
}

// APIProfileNamesForListener returns the profiles currently associated with a
// listener. It is used to prevent a listener from becoming incompatible while
// profiles still identify it as their callback source.
func APIProfileNamesForListener(listenerUUID string) []string {
	profileAPIMu.Lock()
	defer profileAPIMu.Unlock()
	listenerUUID = strings.TrimSpace(listenerUUID)
	names := make([]string, 0)
	for name, profile := range ProfileMap {
		if profile.ListenerUUID == listenerUUID && listenerUUID != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func APIGenerateProfile(name string) error {
	return apiGenerateProfile(name, "", nil, nil)
}

// APIGenerateProfileForBuild compiles a profile with a build job correlation
// ID that is propagated to every build-output event.
func APIGenerateProfileForBuild(name, buildID string) error {
	return apiGenerateProfile(name, strings.TrimSpace(buildID), nil, nil)
}

// APIGenerateProfileForBuildWithOutput additionally routes typed output to the
// supplied job event sink.
func APIGenerateProfileForBuildWithOutput(name, buildID, builder string, publisher func(teamapi.BuildOutput)) error {
	selectedBuilder := strings.TrimSpace(builder)
	return apiGenerateProfile(name, strings.TrimSpace(buildID), &selectedBuilder, publisher)
}

func apiGenerateProfile(name, buildID string, builderOverride *string, publisher func(teamapi.BuildOutput)) error {
	profileAPIMu.Lock()
	profile := ProfileMap[name]
	if profile == nil {
		profileAPIMu.Unlock()
		return errors.New("profile not found")
	}
	copy := cloneProfile(*profile)
	copy.BuildID = buildID
	copy.OutputPublisher = publisher
	if builderOverride != nil {
		copy.Builder = *builderOverride
	}
	profileAPIMu.Unlock()
	return generate(name, &copy)
}

// APISyncProfileDefinition updates the generic build metadata supplied by a
// Lua implant definition. The selected target is preserved while it remains
// supported; otherwise the first registered suggestion becomes the default.
func APISyncProfileDefinition(name, payloadType, builder string, osOptions, archOptions []string) (teamapi.Profile, error) {
	profileAPIMu.Lock()
	defer profileAPIMu.Unlock()
	profile := ProfileMap[name]
	if profile == nil {
		return teamapi.Profile{}, errors.New("profile not found")
	}
	updated := cloneProfile(*profile)
	updated.Type = payloadType
	updated.Builder = builder
	updated.OSOptions = normalizeSuggestions(osOptions)
	updated.ARCHOptions = normalizeSuggestions(archOptions)
	if len(updated.OSOptions) == 0 || len(updated.ARCHOptions) == 0 {
		return teamapi.Profile{}, errors.New("OS and ARCH suggestions are required")
	}
	if !containsSuggestion(updated.OSOptions, updated.OS) {
		updated.OS = updated.OSOptions[0]
	}
	if !containsSuggestion(updated.ARCHOptions, updated.ARCH) {
		updated.ARCH = updated.ARCHOptions[0]
	}
	if err := validateProfile(&updated); err != nil {
		return teamapi.Profile{}, err
	}
	if err := db.DBImplantProfileUpdate(profileToDBRow(name, &updated)); err != nil {
		return teamapi.Profile{}, err
	}
	*profile = updated
	return profileDTO(name, profile), nil
}

func profileDTO(name string, profile *Profile) teamapi.Profile {
	result := teamapi.Profile{
		Name: name, Type: profile.Type, LHOST: profile.LHOST, OS: profile.OS,
		ARCH: profile.ARCH, OSOptions: cloneStrings(profile.OSOptions),
		ARCHOptions: cloneStrings(profile.ARCHOptions), Output: profile.Output,
		Template: profile.Template, PublicKey: profile.PublicKey, Builder: profile.Builder,
		ListenerUUID: profile.ListenerUUID,
	}
	definition, err := db.DBImplantDefinitionGet(name)
	if err != nil {
		result.Protocol = "generic"
		result.Options = json.RawMessage(`{}`)
		return result
	}
	result.Protocol = definition.Protocol
	result.Options = append(json.RawMessage(nil), definition.OptionsJSON...)
	result.OTSConfigured = len(definition.OTSHash) != 0
	result.OTSExpiresAt = cloneTime(definition.OTSExpiresAt)
	result.OTSUsedAt = cloneTime(definition.OTSUsedAt)
	result.ConfigVersion = definition.ConfigVersion
	result.DefinitionCreatedAt = timePointer(definition.CreatedAt)
	result.DefinitionUpdatedAt = timePointer(definition.UpdatedAt)
	return result
}

func applyProfileDTO(profile *Profile, request teamapi.Profile) {
	if request.Type != "" {
		profile.Type = request.Type
	}
	if request.LHOST != "" {
		profile.LHOST = request.LHOST
	}
	if request.OS != "" {
		profile.OS = request.OS
	}
	if request.ARCH != "" {
		profile.ARCH = request.ARCH
	}
	if len(request.OSOptions) != 0 {
		profile.OSOptions = normalizeSuggestions(request.OSOptions)
	}
	if len(request.ARCHOptions) != 0 {
		profile.ARCHOptions = normalizeSuggestions(request.ARCHOptions)
	}
	profile.OSOptions = appendSuggestion(profile.OSOptions, profile.OS)
	profile.ARCHOptions = appendSuggestion(profile.ARCHOptions, profile.ARCH)
	if request.Output != "" {
		profile.Output = request.Output
	}
	if request.Template != "" {
		profile.Template = request.Template
	}
	if request.PublicKey != "" {
		profile.PublicKey = request.PublicKey
	}
	if request.Builder != "" {
		profile.Builder = request.Builder
	}
}

func validateProfile(profile *Profile) error {
	if err := internal.ValidatePayloadType(profile.Type); err != nil {
		return err
	}
	if strings.TrimSpace(profile.OS) == "" || strings.TrimSpace(profile.ARCH) == "" {
		return errors.New("OS and ARCH are required")
	}
	if profile.Builder != "" {
		if err := internal.ValidateCommandName(profile.Builder); err != nil {
			return fmt.Errorf("builder: %w", err)
		}
	}
	return nil
}

func normalizeSuggestions(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = appendSuggestion(result, value)
	}
	return result
}

func profileRequestHasDefinition(request teamapi.Profile) bool {
	return strings.TrimSpace(request.Protocol) != "" || len(request.Options) != 0 ||
		request.OTS != "" || request.OTSExpiresAt != nil
}

func definitionFromProfileRequest(name string, profile *Profile, request teamapi.Profile) (db.ImplantDefinition, error) {
	definition := defaultDefinition(name, profile)
	if protocol := strings.TrimSpace(request.Protocol); protocol != "" {
		definition.Protocol = protocol
	}
	if len(request.Options) != 0 && string(request.Options) != "null" {
		definition.OptionsJSON = string(request.Options)
	}
	if request.OTS != "" {
		digest := sha256.Sum256([]byte(request.OTS))
		definition.OTSHash = append([]byte(nil), digest[:]...)
	}
	definition.OTSExpiresAt = cloneTime(request.OTSExpiresAt)
	return definition, nil
}

func defaultDefinition(name string, profile *Profile) db.ImplantDefinition {
	return db.ImplantDefinition{
		Name:             name,
		Protocol:         "generic",
		PayloadType:      profile.Type,
		OperatingSystems: cloneStrings(profile.OSOptions),
		Architectures:    cloneStrings(profile.ARCHOptions),
		OptionsJSON:      `{}`,
	}
}

func isDefinitionUpdateKey(key string) bool {
	switch key {
	case "PROTOCOL", "OPTIONS", "OTS", "OTS_CLEAR", "OTS_EXPIRES_AT":
		return true
	default:
		return false
	}
}

func updateProfileDefinition(name string, profile *Profile, key, value string) error {
	definition, err := db.DBImplantDefinitionGet(name)
	if errors.Is(err, sql.ErrNoRows) {
		definition = defaultDefinition(name, profile)
	} else if err != nil {
		return err
	}
	switch key {
	case "PROTOCOL":
		value = strings.TrimSpace(value)
		if value == "" {
			return errors.New("protocol is required")
		}
		definition.Protocol = value
	case "OPTIONS":
		if strings.TrimSpace(value) == "" {
			value = `{}`
		}
		definition.OptionsJSON = value
	case "OTS":
		if value == "" {
			return errors.New("OTS must not be empty; use OTS_CLEAR to remove it")
		}
		digest := sha256.Sum256([]byte(value))
		definition.OTSHash = append([]byte(nil), digest[:]...)
		definition.OTSUsedAt = nil
	case "OTS_CLEAR":
		definition.OTSHash = nil
		definition.OTSExpiresAt = nil
		definition.OTSUsedAt = nil
	case "OTS_EXPIRES_AT":
		value = strings.TrimSpace(value)
		if value == "" {
			definition.OTSExpiresAt = nil
			break
		}
		expiresAt, parseErr := time.Parse(time.RFC3339Nano, value)
		if parseErr != nil {
			return fmt.Errorf("OTS expiry must use RFC3339: %w", parseErr)
		}
		definition.OTSExpiresAt = timePointer(expiresAt)
	}
	if key == "PROTOCOL" && profile.ListenerUUID != "" && !strings.EqualFold(definition.Protocol, "http") {
		if err := db.DBImplantDefinitionUpsertAndClearProfileListener(definition); err != nil {
			return err
		}
		profile.ListenerUUID = ""
		return nil
	}
	return db.DBImplantDefinitionUpsert(definition)
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func timePointer(value time.Time) *time.Time {
	copy := value
	return &copy
}
