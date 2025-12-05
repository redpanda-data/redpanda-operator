package fields

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"go/format"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/redpanda-data/redpanda-operator/gen/internal"
)

type properties map[string]property

func (p properties) flatten() []property {
	properties := []property{}
	for _, property := range p {
		properties = append(properties, property)
	}

	sort.SliceStable(properties, func(i, j int) bool {
		return properties[i].Name < properties[j].Name
	})

	return properties
}

type property struct {
	Name         string    `json:"-"`
	OriginalName string    `json:"-"`
	Description  string    `json:"description"`
	Nullable     bool      `json:"nullable"`
	NeedsRestart bool      `json:"needs_restart"`
	Visibility   string    `json:"visibility"`
	IsSecret     bool      `json:"is_secret"`
	Type         string    `json:"type"`
	Units        string    `json:"units"`
	Example      string    `json:"example"`
	Items        *property `json:"items"`
	Aliases      []string  `json:"aliases"`
	EnumValues   []string  `json:"enum_values"`
}

func (p property) GoType(ptr bool) (v string) {
	defer func() {
		if ptr {
			v = "*" + v
		}
	}()

	switch p.Type {
	case "integer":
		return "int64"
	case "boolean":
		return "bool"
	case "number":
		return "float64"
	case "array":
		return "[]" + p.Items.GoType(false)
	case "leaders_preference":
		return "LeadersPreference"
	case "config::throughput_control_group":
		return "ThroughputControlGroup"
	case "config::sasl_mechanisms_override":
		return "SaslMechanismsOverride"
	default:
		if p.IsEnum() {
			return p.Name
		}
		return p.Type
	}
}

func (p property) AliasString() string {
	return p.OriginalName + ";" + strings.Join(p.Aliases, ";")
}

func (p property) EqualityCheck() string {
	if p.Name == "KafkaThroughputControl" || p.Name == "SaslMechanismsOverrides" {
		return "complexArrayEquals(c." + p.Name + ", other." + p.Name + ")"
	}

	switch p.Type {
	case "leaders_preference", "integer", "boolean", "number", "string":
		return "primitiveEquals(c." + p.Name + ", other." + p.Name + ")"
	case "array":
		return "arrayEquals(c." + p.Name + ", other." + p.Name + ")"
	default:
		if p.IsEnum() {
			return "primitiveEquals(c." + p.Name + ", other." + p.Name + ")"
		}
		panic("unhandled equality check type " + p.Type)
	}
}

func (p property) IsEnum() bool {
	return len(p.EnumValues) > 0
}

func (p property) EnumComment() string {
	return "// +kubebuilder:validation:Enum=" + strings.Join(p.EnumValues, ";")
}

func (p property) EnumName() string {
	goType := goName(p.Type)

	if p.Name != goType && p.Type != "string" && p.Type != "recovery_validation_mode" {
		panic(fmt.Sprintf("unhandled enum %s %s", p.Name, p.Type))
	}
	return p.Name
}

func (p property) EnumValueDefinitions() string {
	values := []string{}
	for _, value := range p.EnumValues {
		values = append(values, p.EnumName()+goName(value)+" "+p.EnumName()+" = \""+value+"\"")
	}
	return strings.Join(values, "\n")
}

func (p property) JSONTag() string {
	propertyTag := p.OriginalName
	if len(p.Aliases) != 0 {
		propertyTag += ",aliases:" + p.AliasString()
	}
	return "`json:\"" + strings.ToLower(string(p.Name[0])) + p.Name[1:] + ",omitempty\" property:\"" + propertyTag + "\"`"
}

func (p property) Comment() string {
	baseComment := strings.Join(wrapLine(p.Description), "\n// ")

	baseComment += fmt.Sprintf("\n//\n// requires_restart: %v", p.NeedsRestart)

	if len(p.EnumValues) > 0 {
		baseComment += "\n// allowed_values: [" + strings.Join(p.EnumValues, ", ") + "]"
	}

	if len(p.Example) > 0 {
		baseComment += "\n// example: " + p.Example
	}

	if len(p.Aliases) > 0 {
		baseComment += "\n// aliases: [" + strings.Join(p.Aliases, ", ") + "]"
	}

	return "// " + baseComment + "\n//\n// +optional"
}

type fields struct {
	Properties map[string]property `json:"properties"`
}

type Generator struct{}

func NewGenerator() *Generator { return &Generator{} }

func (g *Generator) Generate(pkg, version string) ([]byte, []byte, error) {
	fields, err := g.fetch(version)
	if err != nil {
		return nil, nil, err
	}

	var fileContents bytes.Buffer
	if err := fieldsGenerator.Execute(&fileContents, map[string]any{
		"Properties": fields.flatten(),
		"Pkg":        pkg,
	}); err != nil {
		return nil, nil, fmt.Errorf("failed to execute template: %w", err)
	}

	formatted, err := format.Source(fileContents.Bytes())
	if err != nil {
		if contextualErrors := internal.ContextualizeFormatErrors(fileContents.Bytes(), err); contextualErrors != "" {
			return nil, nil, errors.New(contextualErrors)
		}
	}

	fileContents.Reset()

	if err := fieldsTestGenerator.Execute(&fileContents, map[string]any{
		"Properties": fields.flatten(),
		"Pkg":        pkg,
	}); err != nil {
		return nil, nil, fmt.Errorf("failed to execute template: %w", err)
	}

	formattedTest, err := format.Source(fileContents.Bytes())
	if err != nil {
		if contextualErrors := internal.ContextualizeFormatErrors(fileContents.Bytes(), err); contextualErrors != "" {
			return nil, nil, errors.New(contextualErrors)
		}
	}

	return formatted, formattedTest, nil
}

func (g *Generator) fetch(version string) (properties, error) {
	if version == "" {
		return nil, fmt.Errorf("version is required")
	}

	url := fmt.Sprintf("https://dl.redpanda.com/public/redpanda/raw/names/redpanda-configuration-schema/versions/%s/configuration_schema.json.gz", version)

	resp, err := http.DefaultClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d fetching %s", resp.StatusCode, url)
	}

	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("creating gzip reader: %w", err)
	}
	defer gz.Close()

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, gz); err != nil {
		return nil, fmt.Errorf("reading gzipped body: %w", err)
	}

	var out fields
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		return nil, fmt.Errorf("decoding json: %w", err)
	}

	properties := make(map[string]property)

	for name, property := range out.Properties {
		if property.Visibility == "user" {
			property.OriginalName = name
			property.Name = goName(name)
			properties[goName(name)] = property
		}
	}

	return properties, nil
}
