package implantbuilder

import (
	"errors"
	"fmt"
	"strings"
	"sync"

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
	updated := *profile
	switch strings.ToUpper(strings.TrimSpace(request.Key)) {
	case "TYPE":
		updated.Type = request.Value
	case "LHOST":
		updated.LHOST = request.Value
	case "OS":
		updated.OS = request.Value
	case "ARCH":
		updated.ARCH = request.Value
	case "URI":
		updated.URI = request.Value
	case "UA":
		updated.UA = request.Value
	case "OUTPUT":
		updated.Output = request.Value
	case "TEMPLATE":
		updated.Template = request.Value
	case "PUBLICKEY":
		updated.PublicKey = request.Value
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

func APIGenerateProfile(name string) error {
	profileAPIMu.Lock()
	profile := ProfileMap[name]
	if profile == nil {
		profileAPIMu.Unlock()
		return errors.New("profile not found")
	}
	copy := *profile
	profileAPIMu.Unlock()
	return generate(name, &copy)
}

func profileDTO(name string, profile *Profile) teamapi.Profile {
	return teamapi.Profile{
		Name: name, Type: profile.Type, LHOST: profile.LHOST, OS: profile.OS,
		ARCH: profile.ARCH, URI: profile.URI, UA: profile.UA, Output: profile.Output,
		Template: profile.Template, PublicKey: profile.PublicKey,
	}
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
	if request.URI != "" {
		profile.URI = request.URI
	}
	if request.UA != "" {
		profile.UA = request.UA
	}
	if request.Output != "" {
		profile.Output = request.Output
	}
	if request.Template != "" {
		profile.Template = request.Template
	}
	if request.PublicKey != "" {
		profile.PublicKey = request.PublicKey
	}
}

func validateProfile(profile *Profile) error {
	if err := internal.ValidatePayloadType(profile.Type); err != nil {
		return err
	}
	if strings.TrimSpace(profile.OS) == "" || strings.TrimSpace(profile.ARCH) == "" {
		return errors.New("OS and ARCH are required")
	}
	if !strings.HasPrefix(profile.URI, "/") {
		return errors.New("URI must begin with /")
	}
	return nil
}
