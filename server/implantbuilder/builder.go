package implantbuilder

import (
"errors"
"fmt"
"os"
"os/exec"
"path/filepath"
"strings"

"github.com/cheynewallace/tabby"
"purpcmd/server/log"
)

// Profile holds the build configuration for a single implant binary.
type Profile struct {
	LHOST    string
	OS       string
	ARCH     string
	URI      string
	UA       string
	Output   string
	Template string
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
		LHOST:    "",
		OS:       "linux",
		ARCH:     "amd64",
		URI:      "/",
		UA:       "Mozilla PurpCMD",
		Output:   "implant",
		Template: "./template",
	}
}

// NewProfile creates a new named profile with defaults and selects it.
// Returns an error if a profile with that name already exists.
func NewProfile(name string) error {
	if _, exists := ProfileMap[name]; exists {
		return fmt.Errorf("profile %q already exists", name)
	}
	ProfileMap[name] = defaultProfile()
	CurrentName = name
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
	copy := p
	ProfileMap[name] = &copy
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
	t.AddHeader("NAME", "LHOST", "OS", "ARCH", "OUTPUT", "ACTIVE")
	for name, p := range ProfileMap {
		active := ""
		if name == CurrentName {
			active = "*"
		}
		t.AddLine(name, p.LHOST, p.OS, p.ARCH, p.Output, active)
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
	t.AddLine("LHOST", p.LHOST, "Listener callback address (host:port)")
	t.AddLine("OS", p.OS, "Target OS (linux, windows, darwin)")
	t.AddLine("ARCH", p.ARCH, "Target architecture (amd64, 386, arm64)")
	t.AddLine("URI", p.URI, "HTTP callback URI path")
	t.AddLine("UA", p.UA, "HTTP User-Agent string")
	t.AddLine("OUTPUT", p.Output, "Output binary filename")
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
	case "TEMPLATE":
		p.Template = value
	default:
		return fmt.Errorf("unknown option: %s", key)
	}
	return nil
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
	if p.LHOST == "" {
		return fmt.Errorf("profile %q: LHOST is not set", name)
	}

	mainSrc := filepath.Join(p.Template, "main.go")
	src, err := os.ReadFile(mainSrc)
	if err != nil {
		return fmt.Errorf("cannot read template main.go: %w", err)
	}

	// Substitute placeholder values in the template source.
	modified := string(src)
	modified = strings.Replace(modified, `"LHOST"`, fmt.Sprintf("%q", p.LHOST), 1)
	modified = strings.Replace(modified, `"/"`, fmt.Sprintf("%q", p.URI), 1)
	modified = strings.Replace(modified, `"Mozilla PurpCMD"`, fmt.Sprintf("%q", p.UA), 1)

	tmpSrc := filepath.Join(p.Template, "main_build.go")
	if err := os.WriteFile(tmpSrc, []byte(modified), 0600); err != nil {
		return fmt.Errorf("cannot write build source: %w", err)
	}
	defer os.Remove(tmpSrc)

	absOutput, err := filepath.Abs(p.Output)
	if err != nil {
		return err
	}
	absTemplateDir, err := filepath.Abs(p.Template)
	if err != nil {
		return err
	}

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
		out = append(out, []string{name, p.OS + "/" + p.ARCH + " -> " + p.Output})
	}
	return out
}
