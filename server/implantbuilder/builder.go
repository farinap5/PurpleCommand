package implantbuilder

import (
	"crypto/rsa"
	"crypto/x509"
	"database/sql"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"purpcmd/internal"
	protocolencrypt "purpcmd/internal/encrypt"
	"purpcmd/pkg/teamapi"
	"purpcmd/server/db"
	"purpcmd/server/log"
	"purpcmd/server/runtimeevents"

	"github.com/cheynewallace/tabby"
)

// Profile holds the build configuration for a single implant binary.
type Profile struct {
	Type            string
	Mode            string
	LHOST           string
	OS              string
	ARCH            string
	OSOptions       []string
	ARCHOptions     []string
	Output          string
	Template        string
	PublicKey       string                    // Path to server public key (e.g., server.pub)
	Builder         string                    // Optional registered Lua payload builder name.
	ListenerUUID    string                    // Optional listener associated with the materialized LHOST.
	ProfileName     string                    // Execution-only profile name; never persisted.
	BuildID         string                    // Execution-only build job ID; never persisted.
	OutputPublisher func(teamapi.BuildOutput) // Execution-only event sink; never persisted.
	OTSToken        [12]byte                  // Execution-only registration token derived from the stored OTS hash.
}

var (
	// ProfileMap stores all named implant profiles.
	ProfileMap = make(map[string]*Profile)
	// CurrentName is the name of the currently selected profile.
	CurrentName string = ""
)

// defaultProfile returns a Profile with sensible defaults.
func defaultProfile() *Profile {
	return &Profile{
		Type:        internal.DefaultPayloadType,
		Mode:        "reverse",
		LHOST:       "",
		OS:          "linux",
		ARCH:        "amd64",
		OSOptions:   []string{"linux"},
		ARCHOptions: []string{"amd64"},
		Output:      "implant",
		Template:    "./template",
		PublicKey:   "server.pub",
	}
}

// NewProfile creates a new named profile with defaults and selects it.
// Returns an error if a profile with that name already exists.
func NewProfile(name string) error {
	if _, exists := ProfileMap[name]; exists {
		return fmt.Errorf("profile %q already exists", name)
	}
	p := defaultProfile()
	ProfileMap[name] = p
	CurrentName = name
	if err := db.DBImplantProfileInsert(profileToDBRow(name, p)); err != nil {
		log.PrintAlert("DB: could not save profile: " + err.Error())
	}
	log.PrintSuccs("New implant profile created: " + name)
	return nil
}

// RegisterProfile upserts a profile from an external source (e.g. Lua).
// If a profile with that name already exists it returns an error — callers
// that want to overwrite must DeleteProfile first.
func RegisterProfile(name string, p Profile) error {
	if _, exists := ProfileMap[name]; exists {
		return fmt.Errorf("profile %q already exists", name)
	}
	if p.Type == "" {
		p.Type = internal.DefaultPayloadType
	}
	if err := validateProfile(&p); err != nil {
		return fmt.Errorf("profile %q: %w", name, err)
	}
	copy := cloneProfile(p)
	ProfileMap[name] = &copy
	if err := db.DBImplantProfileInsert(profileToDBRow(name, &copy)); err != nil {
		log.PrintAlert("DB: could not save profile: " + err.Error())
	}
	log.PrintSuccs("Registered implant profile: " + name)
	return nil
}

// SelectProfile sets the current profile by name.
func SelectProfile(name string) error {
	if _, exists := ProfileMap[name]; !exists {
		return fmt.Errorf("profile %q not found", name)
	}
	CurrentName = name
	log.PrintSuccs("Selected profile: " + name)
	return nil
}

// DeleteProfile removes a profile by name.
func DeleteProfile(name string) error {
	if _, exists := ProfileMap[name]; !exists {
		return fmt.Errorf("profile %q not found", name)
	}
	delete(ProfileMap, name)
	if CurrentName == name {
		CurrentName = ""
	}
	if err := db.DBImplantProfileDelete(name); err != nil {
		log.PrintAlert("DB: could not delete profile: " + err.Error())
	}
	log.PrintSuccs("Deleted profile: " + name)
	return nil
}

// ListProfiles prints all stored profiles in a table.
func ListProfiles() {
	if len(ProfileMap) == 0 {
		log.PrintAlert("no implant profiles")
		return
	}
	t := tabby.New()
	print("\n")
	t.AddHeader("NAME", "TYPE", "MODE", "LHOST", "OS", "ARCH", "OUTPUT", "ACTIVE")
	for name, p := range ProfileMap {
		active := ""
		if name == CurrentName {
			active = "*"
		}
		t.AddLine(name, p.Type, p.Mode, p.LHOST, p.OS, p.ARCH, p.Output, active)
	}
	t.Print()
	print("\n")
}

// ShowOptions prints the options of the currently selected profile.
func ShowOptions() {
	if CurrentName == "" {
		log.PrintErr("no profile selected, run `new profile <name>` or `select <name>` first")
		return
	}
	p := ProfileMap[CurrentName]
	t := tabby.New()
	print("\n")
	println("Profile: " + CurrentName)
	t.AddHeader("OPTION", "VALUE", "DESCRIPTION")
	t.AddLine("TYPE", p.Type, "Payload type used for Lua command routing")
	t.AddLine("MODE", p.Mode, "Transport direction: reverse or bind")
	t.AddLine("LHOST", p.LHOST, "Remote callback address (reverse) or local bind address (bind)")
	t.AddLine("OS", p.OS, "Target OS (linux, windows, darwin)")
	t.AddLine("ARCH", p.ARCH, "Target architecture (amd64, 386, arm64)")
	t.AddLine("OS OPTIONS", strings.Join(p.OSOptions, ", "), "Target suggestions supplied by the implant definition")
	t.AddLine("ARCH OPTIONS", strings.Join(p.ARCHOptions, ", "), "Architecture suggestions supplied by the implant definition")
	t.AddLine("OUTPUT", p.Output, "Output binary filename")
	t.AddLine("PUBLICKEY", p.PublicKey, "Path to server RSA public key file")
	t.AddLine("TEMPLATE", p.Template, "Path to implant template directory")
	t.AddLine("BUILDER", p.Builder, "Registered Lua payload builder (empty uses the legacy builder)")
	t.Print()
	print("\n")
}

// SetOption updates a named option on the currently selected profile.
func SetOption(key, value string) error {
	if CurrentName == "" {
		return errors.New("no profile selected, run `new profile <name>` or `select <name>` first")
	}
	p := ProfileMap[CurrentName]
	switch strings.ToUpper(key) {
	case "TYPE":
		if err := internal.ValidatePayloadType(value); err != nil {
			return err
		}
		p.Type = value
	case "MODE":
		mode, err := normalizeMode(value)
		if err != nil {
			return err
		}
		p.Mode = mode
		if mode == "bind" {
			p.ListenerUUID = ""
		}
	case "LHOST":
		p.LHOST = value
		p.ListenerUUID = ""
	case "OS":
		p.OS = value
		p.OSOptions = appendSuggestion(p.OSOptions, value)
	case "ARCH":
		p.ARCH = value
		p.ARCHOptions = appendSuggestion(p.ARCHOptions, value)
	case "OUTPUT":
		p.Output = value
	case "PUBLICKEY":
		p.PublicKey = value
	case "TEMPLATE":
		p.Template = value
	case "BUILDER":
		if value != "" {
			if err := internal.ValidateCommandName(value); err != nil {
				return fmt.Errorf("builder: %w", err)
			}
		}
		p.Builder = value
	default:
		return fmt.Errorf("unknown option: %s", key)
	}
	if err := db.DBImplantProfileUpdate(profileToDBRow(CurrentName, p)); err != nil {
		log.PrintAlert("DB: could not update profile: " + err.Error())
	}
	return nil
}

// profileToDBRow converts an in-memory profile to a DB row struct.
func profileToDBRow(name string, p *Profile) db.ImplantProfile {
	return db.ImplantProfile{
		Name:         name,
		Type:         p.Type,
		Mode:         p.Mode,
		LHOST:        p.LHOST,
		OS:           p.OS,
		ARCH:         p.ARCH,
		OSOptions:    cloneStrings(p.OSOptions),
		ARCHOptions:  cloneStrings(p.ARCHOptions),
		Output:       p.Output,
		Template:     p.Template,
		PublicKey:    p.PublicKey,
		Builder:      p.Builder,
		ListenerUUID: p.ListenerUUID,
	}
}

// ProfilesReloadFromDB loads all stored profiles from the database into the map.
// Called once at server startup.
func ProfilesReloadFromDB() {
	rows, err := db.DBImplantProfileGetAll()
	if err != nil {
		log.PrintAlert("DB: could not load implant profiles: " + err.Error())
		return
	}
	for _, r := range rows {
		if _, exists := ProfileMap[r.Name]; exists {
			continue // already in map (e.g. from a Lua script that ran first)
		}
		p := &Profile{
			Type:         r.Type,
			Mode:         r.Mode,
			LHOST:        r.LHOST,
			OS:           r.OS,
			ARCH:         r.ARCH,
			OSOptions:    cloneStrings(r.OSOptions),
			ARCHOptions:  cloneStrings(r.ARCHOptions),
			Output:       r.Output,
			Template:     r.Template,
			PublicKey:    r.PublicKey,
			Builder:      r.Builder,
			ListenerUUID: r.ListenerUUID,
		}
		if p.Mode == "" {
			p.Mode = "reverse"
		}
		if p.Type == "" {
			p.Type = internal.DefaultPayloadType
		}
		ProfileMap[r.Name] = p
		log.PrintInfo("Loaded implant profile: " + r.Name)
	}
}

// GenerateByName compiles the implant binary for the named profile.
func GenerateByName(name string) error {
	p, exists := ProfileMap[name]
	if !exists {
		return fmt.Errorf("profile %q not found", name)
	}
	return generate(name, p)
}

// Generate compiles the implant binary for the currently selected profile.
func Generate() error {
	if CurrentName == "" {
		return errors.New("no profile selected, run `new profile <name>` or `select <name>` first")
	}
	return generate(CurrentName, ProfileMap[CurrentName])
}

func generate(name string, p *Profile) error {
	executionProfile := cloneProfile(*p)
	executionProfile.ProfileName = name
	p = &executionProfile
	if err := internal.ValidatePayloadType(p.Type); err != nil {
		return fmt.Errorf("profile %q: %w", name, err)
	}
	mode, err := normalizeMode(p.Mode)
	if err != nil {
		return fmt.Errorf("profile %q: %w", name, err)
	}
	p.Mode = mode
	if p.LHOST == "" {
		return fmt.Errorf("profile %q: LHOST is not set", name)
	}
	definition, definitionErr := db.DBImplantDefinitionGet(name)
	if definitionErr != nil && db.DBMS.DBConn != nil && !errors.Is(definitionErr, sql.ErrNoRows) {
		return fmt.Errorf("profile %q: load one-time secret: %w", name, definitionErr)
	}
	if definitionErr == nil && len(definition.OTSHash) != 0 {
		now := time.Now().UTC()
		if definition.OTSUsedAt != nil {
			return fmt.Errorf("profile %q: one-time secret has already been used", name)
		}
		if definition.OTSExpiresAt != nil && !now.Before(*definition.OTSExpiresAt) {
			return fmt.Errorf("profile %q: one-time secret has expired", name)
		}
		if len(definition.OTSHash) < len(p.OTSToken) {
			return fmt.Errorf("profile %q: stored one-time secret hash is invalid", name)
		}
		length := len(definition.OTSHash)
		if length > len(p.OTSToken) {
			length = len(p.OTSToken)
		}
		copy(p.OTSToken[:], definition.OTSHash[:length])
	}

	absTemplateDir, err := filepath.Abs(p.Template)
	if err != nil {
		return err
	}
	absOutput, err := filepath.Abs(p.Output)
	if err != nil {
		return err
	}
	absPublicKey := ""
	if p.PublicKey != "" {
		absPublicKey, err = filepath.Abs(p.PublicKey)
		if err != nil {
			return err
		}
	}
	if absPublicKey != "" {
		if _, err := loadAndValidatePublicKey(absPublicKey); err != nil {
			return err
		}
	}
	if p.Builder != "" {
		configured := cloneProfile(*p)
		configured.Template = absTemplateDir
		configured.Output = absOutput
		configured.PublicKey = absPublicKey
		return generateWithPayloadBuilder(name, &configured)
	}

	// If the template contains a Makefile, delegate the entire build to make.
	makefilePath := filepath.Join(absTemplateDir, "Makefile")
	if _, err := os.Stat(makefilePath); err == nil {
		return generateWithMakefile(name, p, absTemplateDir, absOutput, absPublicKey)
	}

	return generateGo(name, p, absTemplateDir, absOutput)
}

// generateWithMakefile runs `make` inside the template directory, forwarding all
// profile fields as make variables. The Makefile is responsible for producing the
// final binary at $(OUTPUT).
func generateWithMakefile(name string, p *Profile, absTemplateDir, absOutput, absPublicKey string) error {
	mainSource, err := os.ReadFile(filepath.Join(absTemplateDir, "main.go"))
	if err != nil {
		return fmt.Errorf("cannot read template main.go: %w", err)
	}
	if strings.Contains(string(mainSource), `var publicKeyDER`) && absPublicKey == "" {
		return errors.New("PUBLICKEY is required by the payload source")
	}
	log.PrintInfo(fmt.Sprintf("[%s] Building %s -> %s", name, absTemplateDir, absOutput))

	cmd := exec.Command("make",
		"-C", absTemplateDir,
		fmt.Sprintf("OUTPUT=%s", absOutput),
		fmt.Sprintf("LHOST=%s", p.LHOST),
		fmt.Sprintf("OS=%s", p.OS),
		fmt.Sprintf("ARCH=%s", p.ARCH),
		fmt.Sprintf("TYPE=%s", p.Type),
		fmt.Sprintf("MODE=%s", p.Mode),
		fmt.Sprintf("OTS_BYTES=%s", byteList(p.OTSToken[:])),
		fmt.Sprintf("PUBLICKEY=%s", absPublicKey),
	)
	cmd.Env = append(os.Environ(),
		"GOOS="+p.OS,
		"GOARCH="+p.ARCH,
		"CGO_ENABLED=0",
		"PURPCMD_MODE="+p.Mode,
		"PURPCMD_OTS_TOKEN="+fmt.Sprintf("%x", p.OTSToken),
	)
	if err := runBuildCommand(cmd, *p, "makefile"); err != nil {
		return fmt.Errorf("make build failed: %w", err)
	}

	log.PrintSuccs(fmt.Sprintf("[%s] Implant written to: %s", name, absOutput))
	return nil
}

// generateGo performs the default Go build: substitutes placeholders in main.go,
// writes a temporary main_build.go, compiles it, then removes the temp file.
func generateGo(name string, p *Profile, absTemplateDir, absOutput string) error {
	mainSrc := filepath.Join(absTemplateDir, "main.go")
	src, err := os.ReadFile(mainSrc)
	if err != nil {
		return fmt.Errorf("cannot read template main.go: %w", err)
	}
	modified, err := RenderGoSource(*p, string(src))
	if err != nil {
		return err
	}

	tmpSrc := filepath.Join(absTemplateDir, "main_build.go")
	if err := os.WriteFile(tmpSrc, []byte(modified), 0600); err != nil {
		return fmt.Errorf("cannot write build source: %w", err)
	}
	defer os.Remove(tmpSrc)

	cmd := exec.Command("go", "build", "-ldflags", "-s -w", "-o", absOutput, "main_build.go")
	cmd.Dir = absTemplateDir
	cmd.Env = append(os.Environ(),
		"GOOS="+p.OS,
		"GOARCH="+p.ARCH,
		"CGO_ENABLED=0",
	)
	log.PrintInfo(fmt.Sprintf("[%s] Building implant for %s/%s -> %s", name, p.OS, p.ARCH, p.Output))
	if err := runBuildCommand(cmd, *p, "go"); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}

	log.PrintSuccs(fmt.Sprintf("[%s] Implant written to: %s", name, p.Output))
	return nil
}

func runBuildCommand(command *exec.Cmd, profile Profile, builder string) error {
	var output BuildOutputCapture
	combined := io.MultiWriter(os.Stdout, &output)
	command.Stdout = combined
	command.Stderr = combined
	err := command.Run()
	PublishBuildOutput(profile, builder, output.String())
	return err
}

// PublishBuildOutput emits correlated command output through the teamserver's
// runtime event publisher. Empty output does not create an event record.
func PublishBuildOutput(profile Profile, builder, message string) {
	if strings.TrimSpace(message) == "" {
		return
	}
	output := teamapi.BuildOutput{
		BuildID: profile.BuildID,
		Profile: profile.ProfileName,
		Builder: builder,
		Message: message,
	}
	if profile.OutputPublisher != nil {
		profile.OutputPublisher(output)
		return
	}
	runtimeevents.Publish(teamapi.EventBuildOutput, output)
}

// RenderGoSource applies the standard payload-profile substitutions and embeds
// the configured RSA public key as DER bytes. Both the legacy Go builder and
// Lua os.write use this function so Lua builds preserve the old Makefile's key
// embedding behavior without invoking Python.
func RenderGoSource(profile Profile, source string) (string, error) {
	if strings.Contains(source, `var publicKeyDER`) && strings.TrimSpace(profile.PublicKey) == "" {
		return "", errors.New("PUBLICKEY is required by the payload source")
	}
	var publicKeyDER []byte
	if profile.PublicKey != "" {
		var err error
		publicKeyDER, err = loadAndValidatePublicKey(profile.PublicKey)
		if err != nil {
			return "", err
		}
	}

	modified := strings.Replace(source, `"LHOST"`, fmt.Sprintf("%q", profile.LHOST), 1)
	modified = strings.Replace(modified, `"IMPLANT_TYPE"`, fmt.Sprintf("%q", profile.Type), 1)
	modified = strings.Replace(modified, `"IMPLANT_MODE"`, fmt.Sprintf("%q", profile.Mode), 1)
	otsDeclaration := fmt.Sprintf("[12]byte{%s}", byteList(profile.OTSToken[:]))
	modified = strings.Replace(modified, `var implantOTS [12]byte`, "var implantOTS = "+otsDeclaration, 1)
	if len(publicKeyDER) == 0 {
		return modified, nil
	}
	publicKeyDeclaration := "[]byte{"
	for index, value := range publicKeyDER {
		if index > 0 {
			publicKeyDeclaration += ","
		}
		if index%16 == 0 {
			publicKeyDeclaration += "\n\t\t"
		}
		publicKeyDeclaration += fmt.Sprintf("0x%02x", value)
	}
	publicKeyDeclaration += ",\n\t}"
	modified = strings.Replace(
		modified,
		`var publicKeyDER []byte`,
		"var publicKeyDER = "+publicKeyDeclaration,
		1,
	)
	return modified, nil
}

func loadAndValidatePublicKey(path string) ([]byte, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("PUBLICKEY is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read public key %s: %w", path, err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM block in public key file %s", path)
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("cannot parse public key: %w", err)
	}
	if _, ok := key.(*rsa.PublicKey); !ok {
		return nil, errors.New("public key is not RSA")
	}
	if matches, configured := protocolencrypt.ServerPublicKeyMatchesDER(block.Bytes); configured && !matches {
		return nil, errors.New("payload public key does not match the configured teamserver private key")
	}
	return append([]byte(nil), block.Bytes...), nil
}

func byteList(values []byte) string {
	parts := make([]string, len(values))
	for index, value := range values {
		parts[index] = fmt.Sprintf("0x%02x", value)
	}
	return strings.Join(parts, ",")
}

func normalizeMode(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "reverse":
		return "reverse", nil
	case "bind", "speaker":
		return "bind", nil
	default:
		return "", errors.New("MODE must be reverse or bind")
	}
}

// ProfileNamesForSuggestions returns a slice of [name, description] pairs
// for use by the CLI autocompleter.
func ProfileNamesForSuggestions() [][]string {
	out := make([][]string, 0, len(ProfileMap))
	for name, p := range ProfileMap {
		out = append(out, []string{name, p.Type + " " + p.OS + "/" + p.ARCH + " -> " + p.Output})
	}
	return out
}

func cloneProfile(source Profile) Profile {
	source.OSOptions = cloneStrings(source.OSOptions)
	source.ARCHOptions = cloneStrings(source.ARCHOptions)
	return source
}

func cloneStrings(source []string) []string {
	return append([]string(nil), source...)
}

func appendSuggestion(suggestions []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return suggestions
	}
	for _, suggestion := range suggestions {
		if suggestion == value {
			return suggestions
		}
	}
	return append(suggestions, value)
}

func containsSuggestion(suggestions []string, value string) bool {
	for _, suggestion := range suggestions {
		if suggestion == value {
			return true
		}
	}
	return false
}
