package lua

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"purpcmd/internal"
	"purpcmd/pkg/teamapi"
	"purpcmd/server/db"
	"purpcmd/server/implantbuilder"
	"purpcmd/server/log"
	"purpcmd/server/runtimeevents"

	lua "github.com/yuin/gopher-lua"
)

// ImplantProtocolOptions is the protocol-specific portion of a Lua implant
// definition. It intentionally lives here until the public Team API and build
// profile model support protocol profiles directly.
type ImplantProtocolOptions struct {
	Path    string
	Headers map[string]string
}

// LuaImplantDefinition preserves the definition supplied by Lua. OS and ARCH
// are generic build-target suggestions; protocol options remain isolated from
// the implantbuilder profile.
type LuaImplantDefinition struct {
	Protocol         string
	Type             string
	Builder          string
	OperatingSystems []string
	Architectures    []string
	Options          ImplantProtocolOptions
	OptionsJSON      json.RawMessage
	OTSHash          []byte
}

var (
	luaImplantDefinitionsMu sync.RWMutex
	luaImplantDefinitions   = make(map[string]LuaImplantDefinition)
)

// LuaGetImplantDefinition returns a defensive copy of a registered definition.
// This simulates the protocol registry until its fields become part of the
// public Team API and the implant builder profile.
func LuaGetImplantDefinition(name string) (LuaImplantDefinition, bool) {
	if db.DBMS.DBConn != nil {
		row, err := db.DBImplantDefinitionGet(name)
		if err != nil {
			luaImplantDefinitionsMu.Lock()
			delete(luaImplantDefinitions, name)
			luaImplantDefinitionsMu.Unlock()
			return LuaImplantDefinition{}, false
		}
		definition, err := definitionFromDB(row)
		if err != nil {
			log.PrintErr(fmt.Sprintf("load implant definition %q: %v", name, err))
			return LuaImplantDefinition{}, false
		}
		cacheImplantDefinition(name, definition)
		return cloneDefinition(definition), true
	}

	luaImplantDefinitionsMu.RLock()
	definition, found := luaImplantDefinitions[name]
	luaImplantDefinitionsMu.RUnlock()
	if !found {
		return LuaImplantDefinition{}, false
	}
	return cloneDefinition(definition), true
}

// ImplantDefinitionsReloadFromDB primes the Lua-side registry at startup. The
// database remains the source of truth, so deletes made through implantbuilder
// are reflected by LuaGetImplantDefinition immediately.
func ImplantDefinitionsReloadFromDB() {
	rows, err := db.DBImplantDefinitionGetAll()
	if err != nil {
		log.PrintErr("DB: could not load implant definitions: " + err.Error())
		return
	}
	loaded := make(map[string]LuaImplantDefinition, len(rows))
	for _, row := range rows {
		definition, decodeErr := definitionFromDB(row)
		if decodeErr != nil {
			log.PrintErr(fmt.Sprintf("load implant definition %q: %v", row.Name, decodeErr))
			continue
		}
		loaded[row.Name] = definition
	}
	luaImplantDefinitionsMu.Lock()
	luaImplantDefinitions = loaded
	luaImplantDefinitionsMu.Unlock()
}

// LuaRegisterImplantProfile exposes implant_register_profile(name, table) to Lua.
//
// Lua usage:
//
//	local definition = {
//	    OS = {"linux"},
//	    ARCH = {"amd64", "386"},
//	    PROTOCOL = "http",
//	    TYPE = "impl",
//	    OPTIONS = {
//	        PATH = "/",
//	        HEADER = {
//	            ["User-Agent"] = "",
//	            ["X-Test"] = "Test",
//	        },
//	    },
//	    OTS = "optional-one-time-secret",
//	}
//	implant_register_profile("linux-impl", definition)
func LuaRegisterImplantProfile(L *lua.LState) int {
	name := L.CheckString(1)
	tbl := L.CheckTable(2)

	if isStructuredImplantDefinition(tbl) {
		definition, err := decodeStructuredImplantDefinition(tbl)
		if err != nil {
			return luaImplantProfileError(L, err)
		}
		if db.DBMS.DBConn == nil {
			cacheImplantDefinition(name, definition)
			return 0
		}

		request := teamapi.Profile{
			Name:        name,
			Type:        definition.Type,
			Builder:     definition.Builder,
			OS:          definition.OperatingSystems[0],
			ARCH:        definition.Architectures[0],
			OSOptions:   cloneStrings(definition.OperatingSystems),
			ARCHOptions: cloneStrings(definition.Architectures),
		}
		created, err := syncBuildProfile(request, definition)
		if err != nil {
			return luaImplantProfileError(L, err)
		}
		definition, err = persistImplantDefinition(name, definition)
		if err != nil {
			if created {
				_ = implantbuilder.APIDeleteProfile(name)
			} else {
				publishLuaProfileChange(name, false)
			}
			return luaImplantProfileError(L, err)
		}
		cacheImplantDefinition(name, definition)
		publishLuaProfileChange(name, created)
		log.PrintInfo(fmt.Sprintf(
			"Registered Lua implant definition %q: protocol=%s type=%s targets=%d/%d path=%s headers=%d ots=%t",
			name, definition.Protocol, definition.Type,
			len(definition.OperatingSystems), len(definition.Architectures), definition.Options.Path,
			len(definition.Options.Headers), len(definition.OTSHash) != 0,
		))
		return 0
	}

	return registerLegacyImplantProfile(L, name, tbl)
}

func decodeStructuredImplantDefinition(tbl *lua.LTable) (LuaImplantDefinition, error) {
	operatingSystems, err := luaStringListField(tbl, "OS", []string{"linux"})
	if err != nil {
		return LuaImplantDefinition{}, err
	}
	architectures, err := luaStringListField(tbl, "ARCH", []string{"amd64"})
	if err != nil {
		return LuaImplantDefinition{}, err
	}
	protocol, err := luaStringField(tbl, "PROTOCOL", "http")
	if err != nil {
		return LuaImplantDefinition{}, err
	}
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	if protocol == "" {
		return LuaImplantDefinition{}, errors.New("PROTOCOL is required")
	}

	payloadType, err := luaStringField(tbl, "TYPE", internal.DefaultPayloadType)
	if err != nil {
		return LuaImplantDefinition{}, err
	}
	payloadType = strings.TrimSpace(payloadType)
	if err := internal.ValidatePayloadType(payloadType); err != nil {
		return LuaImplantDefinition{}, err
	}
	builder, err := luaStringField(tbl, "BUILDER", "")
	if err != nil {
		return LuaImplantDefinition{}, err
	}
	builder = strings.TrimSpace(builder)
	if builder != "" {
		if err := internal.ValidateCommandName(builder); err != nil {
			return LuaImplantDefinition{}, fmt.Errorf("BUILDER: %w", err)
		}
	}

	definition := LuaImplantDefinition{
		Protocol:         protocol,
		Type:             payloadType,
		Builder:          builder,
		OperatingSystems: operatingSystems,
		Architectures:    architectures,
		Options: ImplantProtocolOptions{
			Path:    "/",
			Headers: make(map[string]string),
		},
		OptionsJSON: json.RawMessage(`{}`),
	}

	optionsValue := tbl.RawGetString("OPTIONS")
	if optionsValue != lua.LNil {
		options, ok := optionsValue.(*lua.LTable)
		if !ok {
			return LuaImplantDefinition{}, errors.New("OPTIONS must be a table")
		}
		genericOptions, genericErr := luaTableToJSONObject(options)
		if genericErr != nil {
			return LuaImplantDefinition{}, fmt.Errorf("OPTIONS: %w", genericErr)
		}
		definition.OptionsJSON, genericErr = json.Marshal(genericOptions)
		if genericErr != nil {
			return LuaImplantDefinition{}, fmt.Errorf("encode OPTIONS: %w", genericErr)
		}
		path, pathErr := luaStringField(options, "PATH", "/")
		if pathErr != nil {
			return LuaImplantDefinition{}, pathErr
		}
		path = strings.TrimSpace(path)
		if !strings.HasPrefix(path, "/") {
			return LuaImplantDefinition{}, errors.New("OPTIONS.PATH must begin with /")
		}
		definition.Options.Path = path

		headersValue := options.RawGetString("HEADER")
		if headersValue != lua.LNil {
			headers, ok := headersValue.(*lua.LTable)
			if !ok {
				return LuaImplantDefinition{}, errors.New("OPTIONS.HEADER must be a table")
			}
			var headerErr error
			headers.ForEach(func(key, value lua.LValue) {
				if headerErr != nil {
					return
				}
				headerName, ok := key.(lua.LString)
				if !ok || strings.TrimSpace(string(headerName)) == "" {
					headerErr = errors.New("OPTIONS.HEADER keys must be non-empty strings")
					return
				}
				headerValue, ok := value.(lua.LString)
				if !ok {
					headerErr = fmt.Errorf("OPTIONS.HEADER[%q] must be a string", string(headerName))
					return
				}
				canonicalName := http.CanonicalHeaderKey(strings.TrimSpace(string(headerName)))
				definition.Options.Headers[canonicalName] = string(headerValue)
			})
			if headerErr != nil {
				return LuaImplantDefinition{}, headerErr
			}
		}
	}

	otsValue := tbl.RawGetString("OTS")
	if otsValue != lua.LNil {
		ots, ok := otsValue.(lua.LString)
		if !ok {
			return LuaImplantDefinition{}, errors.New("OTS must be a string or nil")
		}
		if ots != "" {
			digest := sha256.Sum256([]byte(ots))
			definition.OTSHash = append([]byte(nil), digest[:]...)
		}
	}

	return definition, nil
}

func registerLegacyImplantProfile(L *lua.LState, name string, tbl *lua.LTable) int {
	p := implantbuilder.Profile{
		Type:        internal.DefaultPayloadType,
		OS:          "linux",
		ARCH:        "amd64",
		OSOptions:   []string{"linux"},
		ARCHOptions: []string{"amd64"},
		Output:      "implant",
		Template:    "./template",
	}
	path := "/"
	userAgent := "Mozilla PurpCMD"

	if v := tbl.RawGetString("type"); v != lua.LNil {
		p.Type = v.String()
	}
	if v := tbl.RawGetString("lhost"); v != lua.LNil {
		p.LHOST = v.String()
	}
	if v := tbl.RawGetString("os"); v != lua.LNil {
		p.OS = v.String()
		p.OSOptions = []string{p.OS}
	}
	if v := tbl.RawGetString("arch"); v != lua.LNil {
		p.ARCH = v.String()
		p.ARCHOptions = []string{p.ARCH}
	}
	if v := tbl.RawGetString("uri"); v != lua.LNil {
		path = v.String()
	}
	if v := tbl.RawGetString("ua"); v != lua.LNil {
		userAgent = v.String()
	}
	if v := tbl.RawGetString("output"); v != lua.LNil {
		p.Output = v.String()
	}
	if v := tbl.RawGetString("template"); v != lua.LNil {
		p.Template = v.String()
	}
	if v := tbl.RawGetString("builder"); v != lua.LNil {
		p.Builder = v.String()
	}

	request := teamapi.Profile{
		Name: name, Type: p.Type, LHOST: p.LHOST, OS: p.OS, ARCH: p.ARCH,
		OSOptions: p.OSOptions, ARCHOptions: p.ARCHOptions, Output: p.Output,
		Template: p.Template, PublicKey: p.PublicKey,
		Builder: p.Builder,
	}
	definition := LuaImplantDefinition{
		Protocol:         "http",
		Type:             p.Type,
		Builder:          p.Builder,
		OperatingSystems: cloneStrings(p.OSOptions),
		Architectures:    cloneStrings(p.ARCHOptions),
		Options: ImplantProtocolOptions{
			Path:    path,
			Headers: map[string]string{"User-Agent": userAgent},
		},
	}
	legacyOptions, _ := json.Marshal(storedProtocolOptions{
		Path: path, Header: map[string]string{"User-Agent": userAgent},
	})
	definition.OptionsJSON = legacyOptions
	if db.DBMS.DBConn == nil {
		cacheImplantDefinition(name, definition)
		return 0
	}
	created, err := syncBuildProfile(request, definition)
	if err != nil {
		return luaImplantProfileError(L, err)
	}
	definition, err = persistImplantDefinition(name, definition)
	if err != nil {
		if created {
			_ = implantbuilder.APIDeleteProfile(name)
		} else {
			publishLuaProfileChange(name, false)
		}
		return luaImplantProfileError(L, err)
	}
	cacheImplantDefinition(name, definition)
	publishLuaProfileChange(name, created)
	return 0
}

func publishLuaProfileChange(name string, created bool) {
	profile, err := implantbuilder.APIGetProfile(name)
	if err != nil {
		return
	}
	eventType := teamapi.EventProfileUpdated
	if created {
		eventType = teamapi.EventProfileCreated
	}
	runtimeevents.Publish(eventType, profile)
}

func isStructuredImplantDefinition(tbl *lua.LTable) bool {
	return tbl.RawGetString("OS") != lua.LNil ||
		tbl.RawGetString("ARCH") != lua.LNil ||
		tbl.RawGetString("PROTOCOL") != lua.LNil ||
		tbl.RawGetString("TYPE") != lua.LNil ||
		tbl.RawGetString("BUILDER") != lua.LNil ||
		tbl.RawGetString("OPTIONS") != lua.LNil ||
		tbl.RawGetString("OTS") != lua.LNil
}

func luaStringListField(tbl *lua.LTable, key string, fallback []string) ([]string, error) {
	value := tbl.RawGetString(key)
	if value == lua.LNil {
		return cloneStrings(fallback), nil
	}
	if text, ok := value.(lua.LString); ok {
		trimmed := strings.TrimSpace(string(text))
		if trimmed == "" {
			return nil, fmt.Errorf("%s must not be empty", key)
		}
		return []string{trimmed}, nil
	}
	values, ok := value.(*lua.LTable)
	if !ok {
		return nil, fmt.Errorf("%s must be a string or array-like table", key)
	}
	length := values.Len()
	if length == 0 {
		return nil, fmt.Errorf("%s must contain at least one suggestion", key)
	}
	seenKeys := 0
	var keyErr error
	values.ForEach(func(itemKey, _ lua.LValue) {
		seenKeys++
		number, numeric := itemKey.(lua.LNumber)
		index := int(number)
		if !numeric || index < 1 || index > length || number != lua.LNumber(index) {
			keyErr = fmt.Errorf("%s must be an array-like table", key)
		}
	})
	if keyErr != nil || seenKeys != length {
		if keyErr != nil {
			return nil, keyErr
		}
		return nil, fmt.Errorf("%s must be a contiguous array-like table", key)
	}
	result := make([]string, 0, length)
	for index := 1; index <= length; index++ {
		text, ok := values.RawGetInt(index).(lua.LString)
		if !ok || strings.TrimSpace(string(text)) == "" {
			return nil, fmt.Errorf("%s[%d] must be a non-empty string", key, index)
		}
		candidate := strings.TrimSpace(string(text))
		duplicate := false
		for _, existing := range result {
			if existing == candidate {
				duplicate = true
				break
			}
		}
		if !duplicate {
			result = append(result, candidate)
		}
	}
	return result, nil
}

func luaTableToJSONObject(table *lua.LTable) (map[string]any, error) {
	result := make(map[string]any)
	var conversionErr error
	table.ForEach(func(key, value lua.LValue) {
		if conversionErr != nil {
			return
		}
		name, ok := key.(lua.LString)
		if !ok || strings.TrimSpace(string(name)) == "" {
			conversionErr = errors.New("object keys must be non-empty strings")
			return
		}
		converted, err := luaValueToJSON(value)
		if err != nil {
			conversionErr = fmt.Errorf("%s: %w", string(name), err)
			return
		}
		result[string(name)] = converted
	})
	return result, conversionErr
}

func luaValueToJSON(value lua.LValue) (any, error) {
	switch typed := value.(type) {
	case lua.LString:
		return string(typed), nil
	case lua.LNumber:
		return float64(typed), nil
	case lua.LBool:
		return bool(typed), nil
	case *lua.LTable:
		length := typed.Len()
		arrayLike := length > 0
		count := 0
		typed.ForEach(func(key, _ lua.LValue) {
			count++
			number, ok := key.(lua.LNumber)
			index := int(number)
			if !ok || index < 1 || index > length || number != lua.LNumber(index) {
				arrayLike = false
			}
		})
		if arrayLike && count == length {
			result := make([]any, length)
			for index := 1; index <= length; index++ {
				converted, err := luaValueToJSON(typed.RawGetInt(index))
				if err != nil {
					return nil, fmt.Errorf("[%d]: %w", index, err)
				}
				result[index-1] = converted
			}
			return result, nil
		}
		return luaTableToJSONObject(typed)
	default:
		if value == lua.LNil {
			return nil, nil
		}
		return nil, fmt.Errorf("%s values cannot be stored as JSON", value.Type().String())
	}
}

func luaStringField(tbl *lua.LTable, key, fallback string) (string, error) {
	value := tbl.RawGetString(key)
	if value == lua.LNil {
		return fallback, nil
	}
	text, ok := value.(lua.LString)
	if !ok {
		return "", fmt.Errorf("%s must be a string", key)
	}
	return string(text), nil
}

func luaImplantProfileError(L *lua.LState, err error) int {
	log.PrintErr("implant_register_profile: " + err.Error())
	L.Push(lua.LString(err.Error()))
	return 1
}

func cloneDefinition(source LuaImplantDefinition) LuaImplantDefinition {
	source.OperatingSystems = cloneStrings(source.OperatingSystems)
	source.Architectures = cloneStrings(source.Architectures)
	source.Options.Headers = cloneHeaders(source.Options.Headers)
	source.OptionsJSON = append(json.RawMessage(nil), source.OptionsJSON...)
	source.OTSHash = append([]byte(nil), source.OTSHash...)
	return source
}

func cloneHeaders(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for name, value := range source {
		result[name] = value
	}
	return result
}

func cloneStrings(source []string) []string {
	return append([]string(nil), source...)
}

type storedProtocolOptions struct {
	Path   string            `json:"path"`
	Header map[string]string `json:"header,omitempty"`
}

func decodeStoredProtocolOptions(value string) (storedProtocolOptions, error) {
	result := storedProtocolOptions{Path: "/", Header: make(map[string]string)}
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(value), &object); err != nil {
		return storedProtocolOptions{}, err
	}
	if object == nil {
		return storedProtocolOptions{}, errors.New("options must be a JSON object")
	}
	for key, raw := range object {
		switch {
		case strings.EqualFold(key, "path"):
			if err := json.Unmarshal(raw, &result.Path); err != nil {
				return storedProtocolOptions{}, fmt.Errorf("%s must be a string", key)
			}
		case strings.EqualFold(key, "header"):
			if err := json.Unmarshal(raw, &result.Header); err != nil {
				return storedProtocolOptions{}, fmt.Errorf("%s must contain string values", key)
			}
		}
	}
	return result, nil
}

func persistImplantDefinition(name string, definition LuaImplantDefinition) (LuaImplantDefinition, error) {
	options := append(json.RawMessage(nil), definition.OptionsJSON...)
	if len(options) == 0 {
		var err error
		options, err = json.Marshal(storedProtocolOptions{
			Path: definition.Options.Path, Header: cloneHeaders(definition.Options.Headers),
		})
		if err != nil {
			return LuaImplantDefinition{}, fmt.Errorf("encode implant definition options: %w", err)
		}
	}
	row := db.ImplantDefinition{
		Name:             name,
		Protocol:         definition.Protocol,
		PayloadType:      definition.Type,
		OperatingSystems: cloneStrings(definition.OperatingSystems),
		Architectures:    cloneStrings(definition.Architectures),
		OptionsJSON:      string(options),
		OTSHash:          append([]byte(nil), definition.OTSHash...),
	}
	existing, getErr := db.DBImplantDefinitionGet(name)
	if getErr == nil {
		// Once registered, protocol options and OTS metadata are managed through
		// the Team API. Script reloads may refresh only build capabilities.
		if existing.PayloadType == row.PayloadType &&
			stringSlicesEqual(existing.OperatingSystems, row.OperatingSystems) &&
			stringSlicesEqual(existing.Architectures, row.Architectures) {
			return definitionFromDB(existing)
		}
		existing.PayloadType = row.PayloadType
		existing.OperatingSystems = row.OperatingSystems
		existing.Architectures = row.Architectures
		existing.UpdatedAt = time.Time{}
		if err := db.DBImplantDefinitionUpsert(existing); err != nil {
			return LuaImplantDefinition{}, err
		}
		stored, err := db.DBImplantDefinitionGet(name)
		if err != nil {
			return LuaImplantDefinition{}, err
		}
		return definitionFromDB(stored)
	}
	if !errors.Is(getErr, sql.ErrNoRows) {
		return LuaImplantDefinition{}, getErr
	}
	if err := db.DBImplantDefinitionUpsert(row); err != nil {
		return LuaImplantDefinition{}, err
	}
	stored, err := db.DBImplantDefinitionGet(name)
	if err != nil {
		return LuaImplantDefinition{}, err
	}
	return definitionFromDB(stored)
}

func stringSlicesEqual(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func definitionFromDB(row db.ImplantDefinition) (LuaImplantDefinition, error) {
	options, err := decodeStoredProtocolOptions(row.OptionsJSON)
	if err != nil {
		return LuaImplantDefinition{}, fmt.Errorf("decode options: %w", err)
	}
	return LuaImplantDefinition{
		Protocol:         row.Protocol,
		Type:             row.PayloadType,
		OperatingSystems: cloneStrings(row.OperatingSystems),
		Architectures:    cloneStrings(row.Architectures),
		Options: ImplantProtocolOptions{
			Path:    options.Path,
			Headers: options.Header,
		},
		OptionsJSON: append(json.RawMessage(nil), row.OptionsJSON...),
		OTSHash:     append([]byte(nil), row.OTSHash...),
	}, nil
}

func cacheImplantDefinition(name string, definition LuaImplantDefinition) {
	luaImplantDefinitionsMu.Lock()
	luaImplantDefinitions[name] = cloneDefinition(definition)
	luaImplantDefinitionsMu.Unlock()
}

// syncBuildProfile maintains generic build metadata shared by the legacy and
// registered Lua builders. Protocol-specific options remain in the definition.
func syncBuildProfile(request teamapi.Profile, definition LuaImplantDefinition) (bool, error) {
	_, err := implantbuilder.APIGetProfile(request.Name)
	created := false
	if err != nil {
		_, err = implantbuilder.APICreateProfile(request)
		if err != nil {
			return false, err
		}
		created = true
	}
	if _, err := implantbuilder.APISyncProfileDefinition(
		request.Name,
		definition.Type,
		definition.Builder,
		definition.OperatingSystems,
		definition.Architectures,
	); err != nil {
		if created {
			_ = implantbuilder.APIDeleteProfile(request.Name)
		}
		return false, err
	}
	return created, nil
}
