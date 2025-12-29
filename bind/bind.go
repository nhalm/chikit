// Package bind provides JSON body decoding with struct validation.
package bind

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/nhalm/chikit/wrapper"
)

var validate = validator.New()

// JSON decodes JSON request body into dest and validates it.
// Returns a *wrapper.Error on decode or validation failure.
func JSON[T any](r *http.Request, dest *T) error {
	if err := json.NewDecoder(r.Body).Decode(dest); err != nil {
		return &wrapper.Error{
			Type:    "request_error",
			Code:    "invalid_json",
			Message: "Invalid JSON in request body",
			Status:  http.StatusBadRequest,
		}
	}

	if err := validate.Struct(dest); err != nil {
		validationErrors, ok := err.(validator.ValidationErrors)
		if !ok {
			return &wrapper.Error{
				Type:    "validation_error",
				Code:    "validation_failed",
				Message: "Validation failed",
				Status:  http.StatusBadRequest,
			}
		}

		fieldErrors := make([]wrapper.FieldError, 0, len(validationErrors))
		for _, ve := range validationErrors {
			fieldErrors = append(fieldErrors, wrapper.FieldError{
				Field:   toJSONFieldName(ve.Field()),
				Code:    ve.Tag(),
				Message: formatValidationMessage(ve),
			})
		}

		return &wrapper.Error{
			Type:    "validation_error",
			Code:    "invalid_request",
			Message: "Validation failed",
			Errors:  fieldErrors,
			Status:  http.StatusBadRequest,
		}
	}

	return nil
}

// RegisterValidation registers a custom validation tag.
func RegisterValidation(tag string, fn validator.Func) error {
	return validate.RegisterValidation(tag, fn)
}

// RegisterAlias registers a validation alias (e.g., "iscolor" -> "hexcolor|rgb|rgba").
func RegisterAlias(alias, tags string) {
	validate.RegisterAlias(alias, tags)
}

func toJSONFieldName(field string) string {
	if field == "" {
		return field
	}
	return strings.ToLower(field[:1]) + field[1:]
}

func formatValidationMessage(ve validator.FieldError) string {
	field := toJSONFieldName(ve.Field())

	switch ve.Tag() {
	case "required":
		return field + " is required"
	case "email":
		return field + " must be a valid email address"
	case "min":
		return field + " must be at least " + ve.Param()
	case "max":
		return field + " must be at most " + ve.Param()
	case "len":
		return field + " must be exactly " + ve.Param() + " characters"
	case "oneof":
		return field + " must be one of: " + ve.Param()
	case "url":
		return field + " must be a valid URL"
	case "uuid":
		return field + " must be a valid UUID"
	case "gt":
		return field + " must be greater than " + ve.Param()
	case "gte":
		return field + " must be greater than or equal to " + ve.Param()
	case "lt":
		return field + " must be less than " + ve.Param()
	case "lte":
		return field + " must be less than or equal to " + ve.Param()
	default:
		return field + " failed validation: " + ve.Tag()
	}
}
