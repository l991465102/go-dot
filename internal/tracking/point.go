package tracking

import (
	"errors"
	"regexp"
	"time"
)

var pointCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{2,127}$`)

type DefinitionState string
type DevelopmentState string
type BehaviorType string
type UVMode string
type TimeBasis string

const (
	Draft          DefinitionState  = "DRAFT"
	Published      DefinitionState  = "PUBLISHED"
	Deprecated     DefinitionState  = "DEPRECATED"
	Pending        DevelopmentState = "PENDING"
	Developing     DevelopmentState = "DEVELOPING"
	Testing        DevelopmentState = "TESTING"
	Online         DevelopmentState = "ONLINE"
	PageView       BehaviorType     = "PAGE_VIEW"
	ElementExpose  BehaviorType     = "ELEMENT_EXPOSE"
	ElementClick   BehaviorType     = "ELEMENT_CLICK"
	BusinessAction BehaviorType     = "BUSINESS_ACTION"
	BusinessResult BehaviorType     = "BUSINESS_RESULT"
	Custom         BehaviorType     = "CUSTOM"
	Approximate    UVMode           = "APPROXIMATE"
	Exact          UVMode           = "EXACT"
	Client         TimeBasis        = "CLIENT"
	Server         TimeBasis        = "SERVER"
)

var (
	ErrPointNotFound     = errors.New("POINT_NOT_FOUND")
	ErrPointNotPublished = errors.New("POINT_NOT_PUBLISHED")
	ErrImmutable         = errors.New("POINT_IMMUTABLE")
	ErrConflict          = errors.New("CONFLICT")
)

type Point struct {
	ID                  uint64           `json:"id"`
	ProjectID           uint64           `json:"project_id"`
	CategoryID          uint64           `json:"category_id"`
	Category            Category         `json:"category"`
	CategoryPath        []CategoryRef    `json:"category_path"`
	Code                string           `json:"code"`
	Name                string           `json:"name"`
	Description         string           `json:"description,omitempty"`
	BehaviorType        BehaviorType     `json:"behavior_type"`
	State               DefinitionState  `json:"definition_state"`
	DevelopmentState    DevelopmentState `json:"development_state"`
	TriggerPage         string           `json:"trigger_page,omitempty"`
	TriggerTiming       string           `json:"trigger_timing,omitempty"`
	TriggerCountRule    string           `json:"trigger_count_rule,omitempty"`
	AbnormalTriggerRule string           `json:"abnormal_trigger_rule,omitempty"`
	CountPV             bool             `json:"count_pv"`
	CountUV             bool             `json:"count_uv"`
	UVMode              UVMode           `json:"uv_mode"`
	TimeBasis           TimeBasis        `json:"time_basis"`
	SupportedPlatforms  []string         `json:"supported_platforms"`
	SupportedChannels   []string         `json:"supported_channels"`
	ProductOwner        string           `json:"product_owner,omitempty"`
	DeveloperOwner      string           `json:"developer_owner,omitempty"`
	RequirementURL      string           `json:"requirement_url,omitempty"`
	Properties          []Property       `json:"properties"`
	PublishedVersion    uint             `json:"published_version,omitempty"`
	DeprecationReason   string           `json:"deprecation_reason,omitempty"`
	ReplacementPointID  *uint64          `json:"replacement_point_id,omitempty"`
	CreatedBy           uint64           `json:"created_by"`
	UpdatedBy           uint64           `json:"updated_by"`
	AuditAction         string           `json:"-"`
}

type DraftInput struct {
	ProjectID           uint64           `json:"project_id,omitempty"`
	CategoryID          uint64           `json:"category_id"`
	Code                string           `json:"code"`
	Name                string           `json:"name"`
	Description         string           `json:"description,omitempty"`
	BehaviorType        BehaviorType     `json:"behavior_type"`
	DevelopmentState    DevelopmentState `json:"development_state,omitempty"`
	TriggerPage         string           `json:"trigger_page,omitempty"`
	TriggerTiming       string           `json:"trigger_timing,omitempty"`
	TriggerCountRule    string           `json:"trigger_count_rule,omitempty"`
	AbnormalTriggerRule string           `json:"abnormal_trigger_rule,omitempty"`
	CountPV             bool             `json:"count_pv"`
	CountUV             bool             `json:"count_uv"`
	UVMode              UVMode           `json:"uv_mode,omitempty"`
	TimeBasis           TimeBasis        `json:"time_basis,omitempty"`
	SupportedPlatforms  []string         `json:"supported_platforms"`
	SupportedChannels   []string         `json:"supported_channels"`
	ProductOwner        string           `json:"product_owner,omitempty"`
	DeveloperOwner      string           `json:"developer_owner,omitempty"`
	RequirementURL      string           `json:"requirement_url,omitempty"`
	Properties          []PropertyInput  `json:"properties"`
}

type Snapshot struct {
	ProjectID           uint64        `json:"project_id"`
	PointID             uint64        `json:"point_id"`
	CategoryID          uint64        `json:"category_id"`
	CategoryCode        string        `json:"category_code"`
	CategoryPath        []CategoryRef `json:"category_path"`
	PointCode           string        `json:"point_code"`
	PointName           string        `json:"point_name"`
	Description         string        `json:"description,omitempty"`
	Version             uint          `json:"version"`
	BehaviorType        BehaviorType  `json:"behavior_type"`
	TriggerPage         string        `json:"trigger_page,omitempty"`
	TriggerTiming       string        `json:"trigger_timing,omitempty"`
	TriggerCountRule    string        `json:"trigger_count_rule,omitempty"`
	AbnormalTriggerRule string        `json:"abnormal_trigger_rule,omitempty"`
	CountPV             bool          `json:"count_pv"`
	CountUV             bool          `json:"count_uv"`
	UVMode              UVMode        `json:"uv_mode"`
	TimeBasis           TimeBasis     `json:"time_basis"`
	SupportedPlatforms  []string      `json:"supported_platforms"`
	SupportedChannels   []string      `json:"supported_channels"`
	ProductOwner        string        `json:"product_owner,omitempty"`
	DeveloperOwner      string        `json:"developer_owner,omitempty"`
	RequirementURL      string        `json:"requirement_url,omitempty"`
	Properties          []Property    `json:"properties"`
	PublishedBy         uint64        `json:"published_by"`
	PublishedAt         time.Time     `json:"published_at"`
}

func (p Point) ValidateForPublication() error {
	if p.Category.Level != Scene || !p.Category.Active || p.Category.ProjectID != p.ProjectID {
		return coded("CATEGORY_INVALID", "point requires an active same-project SCENE category")
	}
	if !pointCodePattern.MatchString(p.Code) || p.Name == "" || len(p.SupportedPlatforms) == 0 {
		return coded("POINT_INVALID", "code must be 3-128 lowercase characters and platforms cannot be empty")
	}
	if !validBehaviorType(p.BehaviorType) {
		return coded("BEHAVIOR_TYPE_INVALID", "unsupported behavior type")
	}
	if !validDevelopmentState(p.DevelopmentState) {
		return coded("DEVELOPMENT_STATE_INVALID", "unsupported development state")
	}
	if !validUVMode(p.UVMode) {
		return coded("UV_MODE_INVALID", "unsupported UV mode")
	}
	if !validTimeBasis(p.TimeBasis) {
		return coded("TIME_BASIS_INVALID", "unsupported time basis")
	}
	for _, platform := range p.SupportedPlatforms {
		if !validPlatform(platform) {
			return coded("PLATFORM_INVALID", "unsupported platform %s", platform)
		}
	}
	return validatePropertySchema(p.Properties)
}

func (p Point) ValidateDraft() error {
	if p.ProjectID == 0 || p.CategoryID == 0 {
		return coded("POINT_INVALID", "project and category are required")
	}
	if p.Category.Level != Scene || !p.Category.Active || p.Category.ProjectID != p.ProjectID {
		return coded("CATEGORY_INVALID", "point requires an active same-project SCENE category")
	}
	if !pointCodePattern.MatchString(p.Code) || p.Name == "" {
		return coded("POINT_INVALID", "point code and name are required")
	}
	if p.BehaviorType != "" && !validBehaviorType(p.BehaviorType) {
		return coded("BEHAVIOR_TYPE_INVALID", "unsupported behavior type")
	}
	if p.DevelopmentState != "" && !validDevelopmentState(p.DevelopmentState) {
		return coded("DEVELOPMENT_STATE_INVALID", "unsupported development state")
	}
	if p.UVMode != "" && !validUVMode(p.UVMode) {
		return coded("UV_MODE_INVALID", "unsupported UV mode")
	}
	if p.TimeBasis != "" && !validTimeBasis(p.TimeBasis) {
		return coded("TIME_BASIS_INVALID", "unsupported time basis")
	}
	for _, platform := range p.SupportedPlatforms {
		if !validPlatform(platform) {
			return coded("PLATFORM_INVALID", "unsupported platform %s", platform)
		}
	}
	return validateDraftPropertySchema(p.Properties)
}

func (p Point) Snapshot(actorID uint64, version uint, publishedAt ...time.Time) Snapshot {
	at := time.Time{}
	if len(publishedAt) > 0 {
		at = publishedAt[0]
	}
	path := p.CategoryPath
	if len(path) == 0 {
		path = []CategoryRef{{ID: p.Category.ID, Code: p.Category.Code, Name: p.Category.Name, Level: p.Category.Level}}
	}
	return Snapshot{ProjectID: p.ProjectID, PointID: p.ID, CategoryID: p.CategoryID, CategoryCode: p.Category.Code, CategoryPath: path, PointCode: p.Code, PointName: p.Name, Description: p.Description, Version: version, BehaviorType: p.BehaviorType, TriggerPage: p.TriggerPage, TriggerTiming: p.TriggerTiming, TriggerCountRule: p.TriggerCountRule, AbnormalTriggerRule: p.AbnormalTriggerRule, CountPV: p.CountPV, CountUV: p.CountUV, UVMode: p.UVMode, TimeBasis: p.TimeBasis, SupportedPlatforms: p.SupportedPlatforms, SupportedChannels: p.SupportedChannels, ProductOwner: p.ProductOwner, DeveloperOwner: p.DeveloperOwner, RequirementURL: p.RequirementURL, Properties: p.Properties, PublishedBy: actorID, PublishedAt: at}
}

func (s Snapshot) ValidateProperties(values map[string]any) error {
	known := make(map[string]Property, len(s.Properties))
	for _, p := range s.Properties {
		known[p.Code] = p
		if p.Required {
			if v, ok := values[p.Code]; !ok || v == nil {
				return coded("PROPERTY_REQUIRED", "property %s", p.Code)
			}
		}
	}
	for code, value := range values {
		p, ok := known[code]
		if !ok {
			return coded("UNKNOWN_PROPERTY", "property %s", code)
		}
		if value == nil {
			if p.Required {
				return coded("PROPERTY_REQUIRED", "property %s", code)
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
	for _, v := range s.SupportedPlatforms {
		if v == platform {
			return s.ValidateProperties(values)
		}
	}
	return coded("PLATFORM_NOT_SUPPORTED", "platform %s", platform)
}
func validDevelopmentState(v DevelopmentState) bool {
	return v == Pending || v == Developing || v == Testing || v == Online
}
func validBehaviorType(v BehaviorType) bool {
	return v == PageView || v == ElementExpose || v == ElementClick || v == BusinessAction || v == BusinessResult || v == Custom
}
func validPlatform(v string) bool {
	return v == "web" || v == "ios" || v == "android" || v == "server" || v == "mini_program"
}
func validUVMode(v UVMode) bool       { return v == Approximate || v == Exact }
func validTimeBasis(v TimeBasis) bool { return v == Client || v == Server }
