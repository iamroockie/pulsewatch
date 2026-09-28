package rest

import (
	"errors"

	"github.com/iamroockie/plinth"

	"github.com/iamroockie/pulsewatch/internal/domain"
)

const codeOutOfRange = "out_of_range"

func toAPIError(err error) error {
	if errors.Is(err, domain.ErrMonitorNotFound) {
		return plinth.NotFoundError(err)
	}

	if violations := plinth.MatchViolations(err, settingsRules()...); len(violations) > 0 {
		return plinth.ValidationError(violations...)
	}

	return err
}

func settingsRules() []plinth.FieldRule {
	return []plinth.FieldRule{
		{
			Err:   domain.ErrInvalidURL,
			Field: "url",
			Code:  "invalid",
		},
		{
			Err:   domain.ErrUnsupportedSchemeURL,
			Field: "url",
			Code:  "unsupported_scheme",
		},
		{
			Err:   domain.ErrCredentialsInURL,
			Field: "url",
			Code:  "credentials_not_allowed",
		},
		{
			Err:    domain.ErrIntervalOutOfRange,
			Field:  "interval_seconds",
			Code:   codeOutOfRange,
			Params: rangeParams(seconds(domain.MinInterval), seconds(domain.MaxInterval)),
		},
		{
			Err:    domain.ErrTimeoutOutOfRange,
			Field:  "timeout_ms",
			Code:   codeOutOfRange,
			Params: rangeParams(domain.MinTimeout.Milliseconds(), domain.MaxTimeout.Milliseconds()),
		},
		{
			Err:   domain.ErrTimeoutExceedsInterval,
			Field: "timeout_ms",
			Code:  "exceeds_interval",
		},
		{
			Err:    domain.ErrMaxRetriesOutOfRange,
			Field:  "max_retries",
			Code:   codeOutOfRange,
			Params: rangeParams(domain.MinRetries, domain.MaxRetries),
		},
	}
}

func rangeParams(lo, hi int64) plinth.Map {
	return plinth.Map{"min": lo, "max": hi}
}
