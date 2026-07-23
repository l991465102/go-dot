package tracking

import (
	"encoding/json"
	"math"
	"reflect"
	"regexp"
	"time"
)

type PropertyType string
type PropertyAnalyticsMode string
type MetricAggregation string

var propertyCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,127}$`)

const (
	String   PropertyType = "STRING"
	Integer  PropertyType = "INTEGER"
	Decimal  PropertyType = "DECIMAL"
	Boolean  PropertyType = "BOOLEAN"
	Datetime PropertyType = "DATETIME"
	Enum     PropertyType = "ENUM"
	Array    PropertyType = "ARRAY"

	AnalyticsNone      PropertyAnalyticsMode = "NONE"
	AnalyticsMetric    PropertyAnalyticsMode = "METRIC"
	AnalyticsDimension PropertyAnalyticsMode = "DIMENSION"

	AggregateCount MetricAggregation = "COUNT"
	AggregateSum   MetricAggregation = "SUM"
	AggregateAvg   MetricAggregation = "AVG"
	AggregateMin   MetricAggregation = "MIN"
	AggregateMax   MetricAggregation = "MAX"
)

type PropertyAnalytics struct {
	Mode         PropertyAnalyticsMode `json:"mode"`
	Aggregations []MetricAggregation   `json:"aggregations,omitempty"`
}

type Property struct {
	Code          string            `json:"code"`
	Name          string            `json:"name"`
	Type          PropertyType      `json:"type"`
	Required      bool              `json:"required"`
	EnumValues    []string          `json:"enum_values,omitempty"`
	ArrayItemType PropertyType      `json:"array_item_type,omitempty"`
	ExampleValue  any               `json:"example_value,omitempty"`
	Description   string            `json:"description,omitempty"`
	Analytics     PropertyAnalytics `json:"analytics"`
	SortOrder     int               `json:"sort_order"`
}

type PropertyInput = Property

func validatePropertySchema(properties []Property) error {
	seen := map[string]struct{}{}
	for _, p := range properties {
		if !propertyCodePattern.MatchString(p.Code) || p.Name == "" {
			return coded("PROPERTY_SCHEMA_INVALID", "property code and name are required")
		}
		if _, ok := seen[p.Code]; ok {
			return coded("PROPERTY_SCHEMA_INVALID", "duplicate property %s", p.Code)
		}
		seen[p.Code] = struct{}{}
		if !validPropertyType(p.Type) {
			return coded("PROPERTY_SCHEMA_INVALID", "invalid type for %s", p.Code)
		}
		if p.Type == Enum {
			if len(p.EnumValues) == 0 {
				return coded("PROPERTY_SCHEMA_INVALID", "enum values required for %s", p.Code)
			}
			values := map[string]struct{}{}
			for _, value := range p.EnumValues {
				if value == "" {
					return coded("PROPERTY_SCHEMA_INVALID", "enum values cannot be empty")
				}
				if _, ok := values[value]; ok {
					return coded("PROPERTY_SCHEMA_INVALID", "enum values must be unique")
				}
				values[value] = struct{}{}
			}
		} else if len(p.EnumValues) != 0 {
			return coded("PROPERTY_SCHEMA_INVALID", "enum values only apply to ENUM")
		}
		if p.Type == Array && !validArrayItemType(p.ArrayItemType) {
			return coded("PROPERTY_SCHEMA_INVALID", "array item type is required for %s", p.Code)
		}
		if p.Type != Array && p.ArrayItemType != "" {
			return coded("PROPERTY_SCHEMA_INVALID", "array item type is only valid for arrays")
		}
		if err := validatePropertyAnalytics(p); err != nil {
			return err
		}
	}
	return nil
}

func validPropertyType(t PropertyType) bool {
	return t == String || t == Integer || t == Decimal || t == Boolean || t == Datetime || t == Enum || t == Array
}

func validateDraftPropertySchema(properties []Property) error {
	seen := map[string]struct{}{}
	for _, p := range properties {
		if !propertyCodePattern.MatchString(p.Code) {
			return coded("PROPERTY_SCHEMA_INVALID", "property code is required and must be valid")
		}
		if _, ok := seen[p.Code]; ok {
			return coded("PROPERTY_SCHEMA_INVALID", "duplicate property %s", p.Code)
		}
		seen[p.Code] = struct{}{}
		if p.Type != "" && !validPropertyType(p.Type) {
			return coded("PROPERTY_SCHEMA_INVALID", "invalid type for %s", p.Code)
		}
		if len(p.EnumValues) > 0 {
			if p.Type != Enum {
				return coded("PROPERTY_SCHEMA_INVALID", "enum values only apply to ENUM")
			}
			values := map[string]struct{}{}
			for _, v := range p.EnumValues {
				if v == "" {
					return coded("PROPERTY_SCHEMA_INVALID", "enum values cannot be empty")
				}
				if _, ok := values[v]; ok {
					return coded("PROPERTY_SCHEMA_INVALID", "enum values must be unique")
				}
				values[v] = struct{}{}
			}
		}
		if p.ArrayItemType != "" && !validArrayItemType(p.ArrayItemType) {
			return coded("PROPERTY_SCHEMA_INVALID", "unsupported array item type")
		}
		if p.Type != "" && p.Type != Array && p.ArrayItemType != "" {
			return coded("PROPERTY_SCHEMA_INVALID", "array item type is only valid for arrays")
		}
		if p.Type == "" {
			if normalizedAnalyticsMode(p.Analytics.Mode) != AnalyticsNone || len(p.Analytics.Aggregations) != 0 {
				return coded("PROPERTY_SCHEMA_INVALID", "property type is required before analytics can be configured")
			}
		} else if err := validatePropertyAnalytics(p); err != nil {
			return err
		}
	}
	return nil
}

func normalizedAnalyticsMode(mode PropertyAnalyticsMode) PropertyAnalyticsMode {
	if mode == "" {
		return AnalyticsNone
	}
	return mode
}

func validatePropertyAnalytics(p Property) error {
	switch normalizedAnalyticsMode(p.Analytics.Mode) {
	case AnalyticsNone:
		if len(p.Analytics.Aggregations) != 0 {
			return coded("PROPERTY_SCHEMA_INVALID", "analytics aggregations require METRIC mode for %s", p.Code)
		}
	case AnalyticsMetric:
		if p.Type != Integer && p.Type != Decimal {
			return coded("PROPERTY_SCHEMA_INVALID", "METRIC analytics require INTEGER or DECIMAL for %s", p.Code)
		}
		if len(p.Analytics.Aggregations) == 0 {
			return coded("PROPERTY_SCHEMA_INVALID", "METRIC analytics require aggregations for %s", p.Code)
		}
		seen := make(map[MetricAggregation]struct{}, len(p.Analytics.Aggregations))
		for _, aggregation := range p.Analytics.Aggregations {
			if !validMetricAggregation(aggregation) {
				return coded("PROPERTY_SCHEMA_INVALID", "unsupported metric aggregation %s for %s", aggregation, p.Code)
			}
			if _, duplicate := seen[aggregation]; duplicate {
				return coded("PROPERTY_SCHEMA_INVALID", "metric aggregations must be unique for %s", p.Code)
			}
			seen[aggregation] = struct{}{}
		}
	case AnalyticsDimension:
		if p.Type != String && p.Type != Enum && p.Type != Boolean {
			return coded("PROPERTY_SCHEMA_INVALID", "DIMENSION analytics require STRING, ENUM, or BOOLEAN for %s", p.Code)
		}
		if len(p.Analytics.Aggregations) != 0 {
			return coded("PROPERTY_SCHEMA_INVALID", "DIMENSION analytics use fixed metrics for %s", p.Code)
		}
	default:
		return coded("PROPERTY_SCHEMA_INVALID", "unsupported analytics mode for %s", p.Code)
	}
	return nil
}

func validMetricAggregation(aggregation MetricAggregation) bool {
	return aggregation == AggregateCount || aggregation == AggregateSum || aggregation == AggregateAvg || aggregation == AggregateMin || aggregation == AggregateMax
}
func validArrayItemType(t PropertyType) bool {
	return t == String || t == Integer || t == Decimal || t == Boolean || t == Datetime
}

func validateValue(code string, typ PropertyType, enums []string, value any) error {
	valid := false
	switch typ {
	case String:
		_, valid = value.(string)
	case Integer:
		switch n := value.(type) {
		case json.Number:
			_, err := n.Int64()
			valid = err == nil
		case float64:
			valid = math.Trunc(n) == n && math.Abs(n) <= 9007199254740991
		case float32:
			valid = math.Trunc(float64(n)) == float64(n)
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			valid = true
		}
	case Decimal:
		switch n := value.(type) {
		case json.Number:
			_, err := n.Float64()
			valid = err == nil
		case float64:
			valid = !math.IsNaN(n) && !math.IsInf(n, 0)
		case float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			valid = true
		}
	case Boolean:
		_, valid = value.(bool)
	case Datetime:
		v, ok := value.(string)
		if ok {
			_, err := time.Parse(time.RFC3339, v)
			valid = err == nil
		}
	case Enum:
		_, valid = value.(string)
	}
	if !valid {
		return coded("PROPERTY_TYPE_ERROR", "property %s must be %s", code, typ)
	}
	if typ == Enum {
		v, ok := value.(string)
		if !ok {
			return coded("PROPERTY_TYPE_ERROR", "enum property %s must be STRING", code)
		}
		for _, allowed := range enums {
			if v == allowed {
				return nil
			}
		}
		return coded("PROPERTY_ENUM_INVALID", "property %s", code)
	}
	return nil
}

func validateArray(code string, itemType PropertyType, value any) error {
	rv := reflect.ValueOf(value)
	if !rv.IsValid() || (rv.Kind() != reflect.Array && rv.Kind() != reflect.Slice) {
		return coded("PROPERTY_TYPE_ERROR", "property %s must be ARRAY", code)
	}
	for i := 0; i < rv.Len(); i++ {
		if err := validateValue(code, itemType, nil, rv.Index(i).Interface()); err != nil {
			return err
		}
	}
	return nil
}
