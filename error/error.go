package error

import (
	"errors"
	"fmt"
)

var OpenApiError = errors.New("OpenApiError")
var OpenApiValidationError = errors.New("Validation error")

func Error(args ...any) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: %w", OpenApiError, errors.New("unknown error"))
	}
	if len(args) == 1 {
		switch args[0].(type) {
		case error:
			err := args[0].(error)
			return fmt.Errorf("%w: %w", OpenApiError, err)
		case string:
			msg := args[0].(string)
			return fmt.Errorf("%w: %w", OpenApiError, errors.New(msg))
		}
	}
	var msg string
	switch args[0].(type) {
	case string:
		msg = args[0].(string)
		args = args[1:]
	}

	errs := make([]error, 0, len(args))
	for _, arg := range args {
		switch arg.(type) {
		case error:
			errs = append(errs, arg.(error))
		case string:
			errs = append(errs, errors.New(arg.(string)))
		}
	}

	if msg != "" {
		err := errors.New(msg)
		if len(errs) > 1 {
			return fmt.Errorf("%w: %w; %w", OpenApiError, err, errors.Join(errs...))
		}
		return fmt.Errorf("%w: %w: %w", OpenApiError, err, errs[0])
	}

	return fmt.Errorf("%w: %w", OpenApiError, errors.Join(errs...))
}

func Errorf(msg string, args ...any) error {
	w := fmt.Errorf(msg, args...)
	return fmt.Errorf("%w: %w", OpenApiError, w)
}

func ErrorFrom(err error) error {
	return Error(err)
}
