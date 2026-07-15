package tracking

import (
	"fmt"
	"math"
	"reflect"
	"time"
)

type PropertyType string

const (
	String   PropertyType = "STRING"
	Integer  PropertyType = "INTEGER"
	Number   PropertyType = "NUMBER"
	Boolean  PropertyType = "BOOLEAN"
	Datetime PropertyType = "DATETIME"
	Array    PropertyType = "ARRAY"
)

type Property struct {
	Code          string       `json:"code"`
	Name          string       `json:"name"`
	Type          PropertyType `json:"type"`
	Required      bool         `json:"required"`
	EnumValues    []string     `json:"enum_values,omitempty"`
	ArrayItemType PropertyType `json:"array_item_type,omitempty"`
	ExampleValue  any          `json:"example_value,omitempty"`
	Description   string       `json:"description,omitempty"`
	SortOrder     int          `json:"sort_order"`
}

type PropertyInput = Property

func validatePropertySchema(properties []Property) error {
	seen := map[string]struct{}{}
	for _, p := range properties {
		if p.Code == "" || p.Name == "" {
			return fmt.Errorf("PROPERTY_SCHEMA_INVALID: property code and name are required")
		}
		if _, ok := seen[p.Code]; ok {
			return fmt.Errorf("PROPERTY_SCHEMA_INVALID: duplicate property %s", p.Code)
		}
		seen[p.Code] = struct{}{}
		if !validScalarType(p.Type) && p.Type != Array {
			return fmt.Errorf("PROPERTY_SCHEMA_INVALID: invalid type for %s", p.Code)
		}
		if p.Type == Array && !validScalarType(p.ArrayItemType) {
			return fmt.Errorf("PROPERTY_SCHEMA_INVALID: array item type is required for %s", p.Code)
		}
		if p.Type != Array && p.ArrayItemType != "" {
			return fmt.Errorf("PROPERTY_SCHEMA_INVALID: array item type is only valid for arrays")
		}
	}
	return nil
}

func validScalarType(t PropertyType) bool {
	return t == String || t == Integer || t == Number || t == Boolean || t == Datetime
}

func validateValue(code string, typ PropertyType, enums []string, value any) error {
	valid := false
	switch typ {
	case String:
		_, valid = value.(string)
	case Integer:
		switch n := value.(type) {
		case float64:
			valid = math.Trunc(n) == n
		case float32:
			valid = math.Trunc(float64(n)) == float64(n)
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			valid = true
		}
	case Number:
		switch value.(type) {
		case float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
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
	}
	if !valid {
		return fmt.Errorf("PROPERTY_TYPE_ERROR: property %s must be %s", code, typ)
	}
	if len(enums) > 0 {
		v, ok := value.(string)
		if !ok {
			return fmt.Errorf("PROPERTY_TYPE_ERROR: enum property %s must be STRING", code)
		}
		for _, allowed := range enums {
			if v == allowed {
				return nil
			}
		}
		return fmt.Errorf("PROPERTY_ENUM_INVALID: property %s", code)
	}
	return nil
}

func validateArray(code string, itemType PropertyType, value any) error {
	rv := reflect.ValueOf(value)
	if !rv.IsValid() || (rv.Kind() != reflect.Array && rv.Kind() != reflect.Slice) {
		return fmt.Errorf("PROPERTY_TYPE_ERROR: property %s must be ARRAY", code)
	}
	for i := 0; i < rv.Len(); i++ {
		if err := validateValue(code, itemType, nil, rv.Index(i).Interface()); err != nil {
			return err
		}
	}
	return nil
}
