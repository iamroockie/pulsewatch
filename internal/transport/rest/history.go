package rest

import (
	"net/http"

	"github.com/iamroockie/plinth"
)

type historyHandler struct {
	svc HistoryService
}

func (h *historyHandler) checks(r *http.Request) (*plinth.Response, error) {
	id, err := plinth.PathUUID(r, "id")
	if err != nil {
		return nil, err
	}

	limit, err := plinth.QueryInt(r, "limit", defaultListLimit)
	if err != nil {
		return nil, err
	}

	before, err := queryTime(r, "before")
	if err != nil {
		return nil, err
	}

	if err := validateLimit(limit); err != nil {
		return nil, err
	}

	page, err := h.svc.Checks(r.Context(), id, before, int(limit))
	if err != nil {
		return nil, toAPIError(err)
	}

	return plinth.NewResponse(http.StatusOK, checkPageFromDomain(page)), nil
}

func (h *historyHandler) uptime(r *http.Request) (*plinth.Response, error) {
	id, err := plinth.PathUUID(r, "id")
	if err != nil {
		return nil, err
	}

	report, err := h.svc.Uptime(r.Context(), id)
	if err != nil {
		return nil, toAPIError(err)
	}

	return plinth.NewResponse(http.StatusOK, uptimeReportFromDomain(report)), nil
}
