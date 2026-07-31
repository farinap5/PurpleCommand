package implantbuilder

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"purpcmd/internal"
	"purpcmd/server/db"
	"purpcmd/server/log"

	"github.com/cheynewallace/tabby"
)

// Profile holds the build configuration for a single implant binary.
type Profile struct {
	Type      string
	LHOST     string
	OS        string
	ARCH      string
	URI       string
	UA        string
	Output    string
	Template  string
	PublicKey string // Path to server public key (e.g., server.pub)
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
		Type:      internal.DefaultPayloadType,
		LHOST:     "",
		OS:        "linux",
		ARCH:      "amd64",
		URI:       "/",
		UA:        "Mozilla PurpCMD",
		Output:    "implant",
		Template:  "./template",
		PublicKey: "server.pub",
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
	if err := internal.ValidatePayloadType(p.Type); err != nil {
		return fmt.Errorf("profile %q: %w", name, err)
	}
	copy := p
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
	t.AddHeader("NAME", "TYPE", "LHOST", "OS", "ARCH", "OUTPUT", "ACTIVE")
	for name, p := range ProfileMap {
		active := ""
		if name == CurrentName {
			active = "*"
		}
		t.AddLine(name, p.Type, p.LHOST, p.OS, p.ARCH, p.Output, active)
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
	t.AddLine("LHOST", p.LHOST, "Listener callback address (host:port)")
	t.AddLine("OS", p.OS, "Target OS (linux, windows, darwin)")
	t.AddLine("ARCH", p.ARCH, "Target architecture (amd64, 386, arm64)")
	t.AddLine("URI", p.URI, "HTTP callback URI path")
	t.AddLine("UA", p.UA, "HTTP User-Agent string")
	t.AddLine("OUTPUT", p.Output, "Output binary filename")
	t.AddLine("PUBLICKEY", p.PublicKey, "Path to server RSA public key file")
	t.AddLine("TEMPLATE", p.Template, "Path to implant template directory")
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
	case "LHOST":
		p.LHOST = value
	case "OS":
		p.OS = value
	case "ARCH":
		p.ARCH = value
	case "URI":
		p.URI = value
	case "UA":
		p.UA = value
	case "OUTPUT":
		p.Output = value
	case "PUBLICKEY":
		p.PublicKey = value
	case "TEMPLATE":
		p.Template = value
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
		Name:      name,
		Type:      p.Type,
		LHOST:     p.LHOST,
		OS:        p.OS,
		ARCH:      p.ARCH,
		URI:       p.URI,
		UA:        p.UA,
		Output:    p.Output,
		Template:  p.Template,
		PublicKey: p.PublicKey,
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
			Type:      r.Type,
			LHOST:     r.LHOST,
			OS:        r.OS,
			ARCH:      r.ARCH,
			URI:       r.URI,
			UA:        r.UA,
			Output:    r.Output,
			Template:  r.Template,
			PublicKey: r.PublicKey,
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
	if err := internal.ValidatePayloadType(p.Type); err != nil {
		return fmt.Errorf("profile %q: %w", name, err)
	}
	if p.LHOST == "" {
		return fmt.Errorf("profile %q: LHOST is not set", name)
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
	log.PrintInfo(fmt.Sprintf("[%s] Building %s -> %s", name, absTemplateDir, absOutput))

	cmd := exec.Command("make",
		"-C", absTemplateDir,
		fmt.Sprintf("OUTPUT=%s", absOutput),
		fmt.Sprintf("LHOST=%s", p.LHOST),
		fmt.Sprintf("OS=%s", p.OS),
		fmt.Sprintf("ARCH=%s", p.ARCH),
		fmt.Sprintf("URI=%s", p.URI),
		fmt.Sprintf("UA=%s", p.UA),
		fmt.Sprintf("TYPE=%s", p.Type),
		fmt.Sprintf("PUBLICKEY=%s", absPublicKey),
	)
	cmd.Env = append(os.Environ(),
		"GOOS="+p.OS,
		"GOARCH="+p.ARCH,
		"CGO_ENABLED=0",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("make build failed: %w", err)
	}

	log.PrintSuccs(fmt.Sprintf("[%s] Implant written to: %s", name, absOutput))
	return nil
}

// generateGo performs the default Go build: substitutes placeholders in main.go,
// writes a temporary main_build.go, compiles it, then removes the temp file.
func generateGo(name string, p *Profile, absTemplateDir, absOutput string) error {
	// Read and validate the public key
	var pubKeyDER []byte
	if p.PublicKey != "" {
		data, err := os.ReadFile(p.PublicKey)
		if err != nil {
			return fmt.Errorf("cannot read public key %s: %w", p.PublicKey, err)
		}
		block, _ := pem.Decode(data)
		if block == nil {
			return fmt.Errorf("no PEM block in public key file %s", p.PublicKey)
		}
		key, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return fmt.Errorf("cannot parse public key: %w", err)
		}
		if _, ok := key.(*rsa.PublicKey); !ok {
			return fmt.Errorf("public key is not RSA")
		}
		pubKeyDER = block.Bytes
	}

	mainSrc := filepath.Join(p.Template, "main.go")
	src, err := os.ReadFile(mainSrc)
	if err != nil {
		return fmt.Errorf("cannot read template main.go: %w", err)
	}

	// Substitute placeholder values in the template source.
	modified := string(src)
	modified = strings.Replace(modified, `"LHOST"`, fmt.Sprintf("%q", p.LHOST), 1)
	modified = strings.Replace(modified, `"IMPLANT_TYPE"`, fmt.Sprintf("%q", p.Type), 1)
	modified = strings.Replace(modified, `"/"`, fmt.Sprintf("%q", p.URI), 1)
	modified = strings.Replace(modified, `"Mozilla PurpCMD"`, fmt.Sprintf("%q", p.UA), 1)

	// Embed the public key as a byte array
	if len(pubKeyDER) > 0 {
		pubKeyStr := "[]byte{"
		for i, b := range pubKeyDER {
			if i > 0 {
				pubKeyStr += ","
			}
			if i%16 == 0 {
				pubKeyStr += "\n\t\t"
			}
			pubKeyStr += fmt.Sprintf("0x%02x", b)
		}
		pubKeyStr += ",\n\t}"
		modified = strings.Replace(modified, `var publicKeyDER []byte`, "var publicKeyDER = "+pubKeyStr, 1)
	}

	tmpSrc := filepath.Join(p.Template, "main_build.go")
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
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	log.PrintInfo(fmt.Sprintf("[%s] Building implant for %s/%s -> %s", name, p.OS, p.ARCH, p.Output))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}

	log.PrintSuccs(fmt.Sprintf("[%s] Implant written to: %s", name, p.Output))
	return nil
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
