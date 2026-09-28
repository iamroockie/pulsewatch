package rest

import (
	"fmt"
	"net/http"
	"time"
	"uuid"

	"github.com/iamroockie/plinth"
)

const (
	defaultListLimit = 50
	minListLimit     = 1
	maxListLimit     = 100
)

func validateLimit(limit int64) error {
	if limit < minListLimit || limit > maxListLimit {
		return plinth.ValidationError(plinth.FieldViolation{
			Field:  "limit",
			Code:   codeOutOfRange,
			Params: rangeParams(minListLimit, maxListLimit),
		})
	}

	return nil
}

func queryUUID(r *http.Request, name string) (uuid.UUID, error) {
	value := r.URL.Query().Get(name)
	if value == "" {
		return uuid.Nil(), nil
	}

	id, err := uuid.Parse(value)
	if err != nil {
		return uuid.Nil(), invalidQueryParam(name, err)
	}

	return id, nil
}

func queryTime(r *http.Request, name string) (time.Time, error) {
	value := r.URL.Query().Get(name)
	if value == "" {
		return time.Time{}, nil
	}

	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, invalidQueryParam(name, err)
	}

	return t, nil
}

func invalidQueryParam(name string, err error) error {
	return plinth.BadRequestError(fmt.Sprintf("invalid query parameter %q", name), err)
}
