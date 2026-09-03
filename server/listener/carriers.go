package listener

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const maxImageTemplateSize = 4 << 20

var (
	imageCarrierMarker = []byte("PURPLECOMMAND-CARRIER-V1")
	defaultPNGTemplate = []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x44, 0x41,
		0x54, 0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0xf0,
		0x1f, 0x00, 0x05, 0x00, 0x01, 0xff, 0x89, 0x99,
		0x3d, 0x1d, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45,
		0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
	}
)

type bodyCarrier struct{}

func (bodyCarrier) Definition() CarrierDefinition {
	return CarrierDefinition{ID: "body", Description: "Place callback data directly in the HTTP entity body."}
}
func (bodyCarrier) Validate(options json.RawMessage) error {
	return validateEmptyCarrierOptions(options)
}
func (bodyCarrier) Decode(envelope CarrierEnvelope, _ json.RawMessage) (CarriedMessage, error) {
	return CarriedMessage{Payload: append([]byte(nil), envelope.Body...)}, nil
}
func (bodyCarrier) Encode(message CarriedMessage, _ json.RawMessage) (CarrierEnvelope, error) {
	return CarrierEnvelope{Body: append([]byte(nil), message.Payload...)}, nil
}

type namedFieldCarrier struct {
	id          string
	prefix      string
	description string
}

type namedFieldOptions struct {
	Name string `json:"name"`
}

func (carrier namedFieldCarrier) Definition() CarrierDefinition {
	return CarrierDefinition{
		ID: carrier.id, Description: carrier.description,
		Options: []OptionDefinition{{Key: "name", Type: OptionString, Required: true, Description: "HTTP field name."}},
	}
}

func (carrier namedFieldCarrier) Validate(options json.RawMessage) error {
	_, err := carrier.decodeOptions(options)
	return err
}

func (carrier namedFieldCarrier) Decode(envelope CarrierEnvelope, options json.RawMessage) (CarriedMessage, error) {
	configuration, err := carrier.decodeOptions(options)
	if err != nil {
		return CarriedMessage{}, err
	}
	values := envelope.Fields[carrier.key(configuration.Name)]
	if len(values) == 0 {
		return CarriedMessage{}, fmt.Errorf("missing %s %q", carrier.id, configuration.Name)
	}
	return CarriedMessage{Payload: []byte(values[0])}, nil
}

func (carrier namedFieldCarrier) Encode(message CarriedMessage, options json.RawMessage) (CarrierEnvelope, error) {
	configuration, err := carrier.decodeOptions(options)
	if err != nil {
		return CarrierEnvelope{}, err
	}
	return CarrierEnvelope{Fields: map[string][]string{carrier.key(configuration.Name): {string(message.Payload)}}}, nil
}

func (carrier namedFieldCarrier) decodeOptions(options json.RawMessage) (namedFieldOptions, error) {
	configuration, err := decodeNamedFieldOptions(options)
	if err != nil {
		return configuration, err
	}
	if (carrier.id == "header" || carrier.id == "cookie") && !validHTTPToken(configuration.Name) {
		return configuration, fmt.Errorf("invalid HTTP %s name %q", carrier.id, configuration.Name)
	}
	return configuration, nil
}

func (carrier namedFieldCarrier) key(name string) string {
	if carrier.id == "header" {
		name = strings.ToLower(name)
	}
	return carrier.prefix + name
}

func decodeNamedFieldOptions(options json.RawMessage) (namedFieldOptions, error) {
	var configuration namedFieldOptions
	if err := decodeStrictJSONObject(options, &configuration); err != nil {
		return configuration, err
	}
	configuration.Name = strings.TrimSpace(configuration.Name)
	if configuration.Name == "" || strings.ContainsAny(configuration.Name, "\r\n") {
		return configuration, errors.New("carrier field name is required and may not contain line breaks")
	}
	return configuration, nil
}

type imageCarrier struct{}

type imageCarrierOptions struct {
	TemplateBase64 string `json:"template_base64,omitempty"`
}

func (imageCarrier) Definition() CarrierDefinition {
	return CarrierDefinition{
		ID: "image", Description: "Append callback data to a valid PNG container.",
		Options: []OptionDefinition{{Key: "template_base64", Type: OptionString, Description: "Optional base64-encoded PNG template (maximum 4 MiB)."}},
	}
}

func (imageCarrier) Validate(options json.RawMessage) error {
	_, err := decodeImageCarrierOptions(options)
	return err
}

func (imageCarrier) Decode(envelope CarrierEnvelope, options json.RawMessage) (CarriedMessage, error) {
	if _, err := decodeImageCarrierOptions(options); err != nil {
		return CarriedMessage{}, err
	}
	if len(envelope.Body) < 8 || !bytes.Equal(envelope.Body[:8], defaultPNGTemplate[:8]) {
		return CarriedMessage{}, errors.New("image carrier body is not a PNG")
	}
	index := bytes.LastIndex(envelope.Body, imageCarrierMarker)
	if index < 0 || len(envelope.Body)-index < len(imageCarrierMarker)+4 {
		return CarriedMessage{}, errors.New("image carrier marker is missing")
	}
	lengthOffset := index + len(imageCarrierMarker)
	length := binary.BigEndian.Uint32(envelope.Body[lengthOffset : lengthOffset+4])
	payloadOffset := lengthOffset + 4
	if uint64(payloadOffset)+uint64(length) != uint64(len(envelope.Body)) {
		return CarriedMessage{}, errors.New("image carrier payload length is invalid")
	}
	return CarriedMessage{Payload: append([]byte(nil), envelope.Body[payloadOffset:]...)}, nil
}

func validHTTPToken(value string) bool {
	if value == "" {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if character <= 0x20 || character >= 0x7f || strings.ContainsRune("()<>@,;:\\\"/[]?={}", rune(character)) {
			return false
		}
	}
	return true
}

func (imageCarrier) Encode(message CarriedMessage, options json.RawMessage) (CarrierEnvelope, error) {
	template, err := decodeImageCarrierOptions(options)
	if err != nil {
		return CarrierEnvelope{}, err
	}
	if uint64(len(message.Payload)) > uint64(^uint32(0)) {
		return CarrierEnvelope{}, errors.New("image carrier payload is too large")
	}
	body := make([]byte, 0, len(template)+len(imageCarrierMarker)+4+len(message.Payload))
	body = append(body, template...)
	body = append(body, imageCarrierMarker...)
	length := make([]byte, 4)
	binary.BigEndian.PutUint32(length, uint32(len(message.Payload)))
	body = append(body, length...)
	body = append(body, message.Payload...)
	return CarrierEnvelope{
		Body:   body,
		Fields: map[string][]string{"header:content-type": {"image/png"}},
	}, nil
}

func decodeImageCarrierOptions(options json.RawMessage) ([]byte, error) {
	var configuration imageCarrierOptions
	if err := decodeStrictJSONObject(options, &configuration); err != nil {
		return nil, err
	}
	if configuration.TemplateBase64 == "" {
		return append([]byte(nil), defaultPNGTemplate...), nil
	}
	template, err := base64.StdEncoding.Strict().DecodeString(configuration.TemplateBase64)
	if err != nil {
		return nil, fmt.Errorf("decode image template: %w", err)
	}
	if len(template) > maxImageTemplateSize {
		return nil, fmt.Errorf("image template exceeds %d bytes", maxImageTemplateSize)
	}
	if len(template) < 8 || !bytes.Equal(template[:8], defaultPNGTemplate[:8]) {
		return nil, errors.New("image template is not a PNG")
	}
	return template, nil
}

func validateEmptyCarrierOptions(options json.RawMessage) error {
	var decoded map[string]json.RawMessage
	if err := decodeStrictJSONObject(options, &decoded); err != nil {
		return err
	}
	if len(decoded) != 0 {
		return errors.New("carrier does not accept options")
	}
	return nil
}

func decodeStrictJSONObject(data json.RawMessage, destination any) error {
	if len(data) == 0 || string(data) == "null" {
		data = json.RawMessage(`{}`)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("unexpected trailing JSON data")
		}
		return err
	}
	return nil
}

func registerHTTPCarriers(registry *Registry) error {
	carriers := []Carrier{
		bodyCarrier{},
		namedFieldCarrier{id: "cookie", prefix: "cookie:", description: "Place callback data in an HTTP cookie."},
		namedFieldCarrier{id: "header", prefix: "header:", description: "Place callback data in an HTTP header."},
		namedFieldCarrier{id: "query", prefix: "query:", description: "Read callback data from an HTTP query parameter."},
		imageCarrier{},
	}
	for _, carrier := range carriers {
		if err := registry.RegisterCarrier(carrier); err != nil {
			return err
		}
	}
	return nil
}
