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

// ImplantBuilder holds the build configuration for a new implant binary.
type ImplantBuilder struct {
	LHOST    string
	OS       string
	ARCH     string
	URI      string
	UA       string
	Output   string
	Template string
}

// Current is the active implant builder configuration.
var Current *ImplantBuilder

// New initialises Current with default values.
func New() {
	Current = &ImplantBuilder{
		LHOST:    "",
		OS:       "linux",
		ARCH:     "amd64",
		URI:      "/",
		UA:       "Mozilla PurpCMD",
		Output:   "implant",
		Template: "./template",
	}
	log.PrintSuccs("New implant configuration created")
}

// ShowOptions prints the current builder options.
func ShowOptions() {
	if Current == nil {
		log.PrintErr("no active implant configuration, run `new` first")
		return
	}
	t := tabby.New()
	print("\n")
	t.AddHeader("OPTION", "VALUE", "DESCRIPTION")
	t.AddLine("LHOST", Current.LHOST, "Listener callback address (host:port)")
	t.AddLine("OS", Current.OS, "Target OS (linux, windows, darwin)")
	t.AddLine("ARCH", Current.ARCH, "Target architecture (amd64, 386, arm64)")
	t.AddLine("URI", Current.URI, "HTTP callback URI path")
	t.AddLine("UA", Current.UA, "HTTP User-Agent string")
	t.AddLine("OUTPUT", Current.Output, "Output binary filename")
	t.AddLine("TEMPLATE", Current.Template, "Path to implant template directory")
	t.Print()
	print("\n")
}

// SetOption updates a named option on the current builder.
func SetOption(key, value string) error {
	if Current == nil {
		return errors.New("no active implant configuration, run `new` first")
	}
	switch strings.ToUpper(key) {
	case "LHOST":
		Current.LHOST = value
	case "OS":
		Current.OS = value
	case "ARCH":
		Current.ARCH = value
	case "URI":
		Current.URI = value
	case "UA":
		Current.UA = value
	case "OUTPUT":
		Current.Output = value
	case "TEMPLATE":
		Current.Template = value
	default:
		return fmt.Errorf("unknown option: %s", key)
	}
	return nil
}

// Generate compiles the implant binary using the template and current options.
func Generate() error {
	if Current == nil {
		return errors.New("no active implant configuration, run `new` first")
	}
	if Current.LHOST == "" {
		return errors.New("LHOST is not set")
	}

	mainSrc := filepath.Join(Current.Template, "main.go")
	src, err := os.ReadFile(mainSrc)
	if err != nil {
		return fmt.Errorf("cannot read template main.go: %w", err)
	}

	// Substitute placeholder values in the template source.
	modified := string(src)
	modified = strings.Replace(modified, `"LHOST"`, fmt.Sprintf("%q", Current.LHOST), 1)
	modified = strings.Replace(modified, `"/"`, fmt.Sprintf("%q", Current.URI), 1)
	modified = strings.Replace(modified, `"Mozilla PurpCMD"`, fmt.Sprintf("%q", Current.UA), 1)

	tmpSrc := filepath.Join(Current.Template, "main_build.go")
	if err := os.WriteFile(tmpSrc, []byte(modified), 0600); err != nil {
		return fmt.Errorf("cannot write build source: %w", err)
	}
	defer os.Remove(tmpSrc)

	absOutput, err := filepath.Abs(Current.Output)
	if err != nil {
		return err
	}

	absTemplateDir, err := filepath.Abs(Current.Template)
	if err != nil {
		return err
	}

	cmd := exec.Command("go", "build", "-ldflags", "-s -w", "-o", absOutput, "main_build.go")
	cmd.Dir = absTemplateDir
	cmd.Env = append(os.Environ(),
		"GOOS="+Current.OS,
		"GOARCH="+Current.ARCH,
		"CGO_ENABLED=0",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	log.PrintInfo(fmt.Sprintf("Building implant for %s/%s -> %s", Current.OS, Current.ARCH, Current.Output))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}

	log.PrintSuccs(fmt.Sprintf("Implant written to: %s", Current.Output))
	return nil
}
