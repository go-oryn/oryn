package validator

import (
	"reflect"
	"strings"

	playground "github.com/go-playground/validator/v10"
)

// Shared messages.
const (
	msgRequired = "is required"
	msgURL      = "must be a valid URL"
	msgUUID     = "must be a valid UUID"
	msgNumber   = "must be a number"
)

// messages are the messages of the tags without parameter.
var messages = map[string]string{
	"required":             msgRequired,
	"required_if":          msgRequired,
	"required_unless":      msgRequired,
	"required_with":        msgRequired,
	"required_with_all":    msgRequired,
	"required_without":     msgRequired,
	"required_without_all": msgRequired,
	"email":                "must be a valid email address",
	"url":                  msgURL,
	"http_url":             msgURL,
	"uuid":                 msgUUID,
	"uuid4":                msgUUID,
	"uuid7":                msgUUID,
	"alpha":                "must contain only letters",
	"alphanum":             "must contain only letters and digits",
	"numeric":              msgNumber,
	"number":               msgNumber,
}

// paramMessages are the messages of the tags with a parameter, followed by the parameter.
var paramMessages = map[string]string{
	"gt":       "must be greater than ",
	"gte":      "must be greater than or equal to ",
	"lt":       "must be less than ",
	"lte":      "must be less than or equal to ",
	"eq":       "must be equal to ",
	"ne":       "must not be equal to ",
	"eqfield":  "must be equal to ",
	"nefield":  "must not be equal to ",
	"datetime": "must be a date time with the format ",
}

// sizeMessages are the messages of the size tags, followed by the parameter and its unit.
var sizeMessages = map[string]string{
	"len": "must be exactly ",
	"min": "must be at least ",
	"max": "must be at most ",
}

// message returns an English message for the failed tag. Unknown tags get a generic message.
func message(err playground.FieldError) string {
	tag, param := err.Tag(), err.Param()

	if msg, ok := messages[tag]; ok {
		return msg
	}

	if prefix, ok := paramMessages[tag]; ok {
		return prefix + param
	}

	if prefix, ok := sizeMessages[tag]; ok {
		return prefix + param + sizeUnit(err.Kind())
	}

	switch {
	case tag == "oneof":
		return "must be one of: " + strings.Join(strings.Fields(param), ", ")
	case param != "":
		return "must satisfy " + tag + "=" + param
	default:
		return "must satisfy " + tag
	}
}

// sizeUnit returns the unit of a size: a length for strings, a number of items for
// collections, nothing for numbers.
func sizeUnit(kind reflect.Kind) string {
	switch kind { //nolint:exhaustive // Other kinds are values.
	case reflect.String:
		return " characters long"
	case reflect.Slice, reflect.Array, reflect.Map:
		return " items long"
	default:
		return ""
	}
}
