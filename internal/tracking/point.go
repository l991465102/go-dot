package tracking

import (
	"errors"
	"fmt"
	"regexp"
	"time"
)

var pointCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

type DefinitionState string

const (
	Draft      DefinitionState = "DRAFT"
	Published  DefinitionState = "PUBLISHED"
	Deprecated DefinitionState = "DEPRECATED"
)

var (
	ErrPointNotFound     = errors.New("POINT_NOT_FOUND")
	ErrPointNotPublished = errors.New("POINT_NOT_PUBLISHED")
	ErrImmutable         = errors.New("published tracking point is immutable")
)

type Point struct {
	ID                  uint64          `json:"id"`
	ProjectID           uint64          `json:"project_id"`
	CategoryID          uint64          `json:"category_id"`
	Category            Category        `json:"category"`
	Code                string          `json:"code"`
	Name                string          `json:"name"`
	Description         string          `json:"description,omitempty"`
	BehaviorType        string          `json:"behavior_type"`
	State               DefinitionState `json:"definition_state"`
	DevelopmentState    string          `json:"development_state"`
	TriggerPage         string          `json:"trigger_page,omitempty"`
	TriggerTiming       string          `json:"trigger_timing,omitempty"`
	TriggerCountRule    string          `json:"trigger_count_rule,omitempty"`
	AbnormalTriggerRule string          `json:"abnormal_trigger_rule,omitempty"`
	CountPV             bool            `json:"count_pv"`
	CountUV             bool            `json:"count_uv"`
	UVMode              string          `json:"uv_mode"`
	TimeBasis           string          `json:"time_basis"`
	SupportedPlatforms  []string        `json:"supported_platforms"`
	SupportedChannels   []string        `json:"supported_channels"`
	ProductOwner        string          `json:"product_owner,omitempty"`
	DeveloperOwner      string          `json:"developer_owner,omitempty"`
	RequirementURL      string          `json:"requirement_url,omitempty"`
	Properties          []Property      `json:"properties"`
	PublishedVersion    uint            `json:"published_version,omitempty"`
	DeprecationReason   string          `json:"deprecation_reason,omitempty"`
	ReplacementPointID  *uint64         `json:"replacement_point_id,omitempty"`
	CreatedBy           uint64          `json:"created_by"`
	UpdatedBy           uint64          `json:"updated_by"`
}

type DraftInput struct {
	ProjectID           uint64          `json:"project_id"`
	CategoryID          uint64          `json:"category_id"`
	Code                string          `json:"code"`
	Name                string          `json:"name"`
	Description         string          `json:"description,omitempty"`
	BehaviorType        string          `json:"behavior_type"`
	DevelopmentState    string          `json:"development_state,omitempty"`
	TriggerPage         string          `json:"trigger_page,omitempty"`
	TriggerTiming       string          `json:"trigger_timing,omitempty"`
	TriggerCountRule    string          `json:"trigger_count_rule,omitempty"`
	AbnormalTriggerRule string          `json:"abnormal_trigger_rule,omitempty"`
	CountPV             bool            `json:"count_pv"`
	CountUV             bool            `json:"count_uv"`
	UVMode              string          `json:"uv_mode,omitempty"`
	TimeBasis           string          `json:"time_basis,omitempty"`
	SupportedPlatforms  []string        `json:"supported_platforms"`
	SupportedChannels   []string        `json:"supported_channels"`
	ProductOwner        string          `json:"product_owner,omitempty"`
	DeveloperOwner      string          `json:"developer_owner,omitempty"`
	RequirementURL      string          `json:"requirement_url,omitempty"`
	Properties          []PropertyInput `json:"properties"`
}

type Snapshot struct {
	ID                 uint64     `json:"id"`
	ProjectID          uint64     `json:"project_id"`
	PointID            uint64     `json:"point_id"`
	PointCode          string     `json:"point_code"`
	Version            uint       `json:"version"`
	BehaviorType       string     `json:"behavior_type"`
	SupportedPlatforms []string   `json:"supported_platforms"`
	SupportedChannels  []string   `json:"supported_channels"`
	Properties         []Property `json:"properties"`
	PublishedBy        uint64     `json:"published_by"`
	PublishedAt        time.Time  `json:"published_at"`
}

func (p Point) ValidateForPublication() error {
	if p.Category.Level != Scene || !p.Category.Active {
		return fmt.Errorf("CATEGORY_INVALID: publication requires an active SCENE category")
	}
	if !pointCodePattern.MatchString(p.Code) || p.Name == "" || p.BehaviorType == "" || len(p.SupportedPlatforms) == 0 {
		return fmt.Errorf("POINT_INVALID: code, name, behavior_type and supported_platforms are required")
	}
	return validatePropertySchema(p.Properties)
}

func (p Point) Snapshot(actorID uint64, version uint) Snapshot {
	return Snapshot{ProjectID: p.ProjectID, PointID: p.ID, PointCode: p.Code, Version: version, BehaviorType: p.BehaviorType, SupportedPlatforms: p.SupportedPlatforms, SupportedChannels: p.SupportedChannels, Properties: p.Properties, PublishedBy: actorID}
}

func (s Snapshot) ValidateProperties(values map[string]any) error {
	known := make(map[string]Property, len(s.Properties))
	for _, p := range s.Properties {
		known[p.Code] = p
		if p.Required {
			if value, ok := values[p.Code]; !ok || value == nil {
				return fmt.Errorf("PROPERTY_REQUIRED: property %s", p.Code)
			}
		}
	}
	for code, value := range values {
		p, ok := known[code]
		if !ok {
			return fmt.Errorf("UNKNOWN_PROPERTY: property %s", code)
		}
		if value == nil {
			if p.Required {
				return fmt.Errorf("PROPERTY_REQUIRED: property %s", code)
			}
			continue
		}
		if p.Type == Array {
			if err := validateArray(code, p.ArrayItemType, value); err != nil {
				return err
			}
		} else if err := validateValue(code, p.Type, p.EnumValues, value); err != nil {
			return err
		}
	}
	return nil
}

func (s Snapshot) Validate(platform string, values map[string]any) error {
	if s.PointID == 0 || s.Version == 0 {
		return ErrPointNotPublished
	}
	for _, supported := range s.SupportedPlatforms {
		if supported == platform {
			return s.ValidateProperties(values)
		}
	}
	return fmt.Errorf("PLATFORM_NOT_SUPPORTED: platform %s", platform)
}
