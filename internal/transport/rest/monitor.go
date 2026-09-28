package rest

import (
	"encoding/json/v2"
	"net/http"

	"github.com/iamroockie/plinth"
)

type monitorHandler struct {
	svc MonitorService
}

func (h *monitorHandler) create(r *http.Request) (*plinth.Response, error) {
	req, err := parseRequest[createMonitorRequest](r)
	if err != nil {
		return nil, err
	}

	m, err := h.svc.Create(r.Context(), req.toDomain())
	if err != nil {
		return nil, toAPIError(err)
	}

	resp := plinth.NewResponse(http.StatusCreated, monitorFromDomain(m))

	return resp.SetHeader("Location", r.URL.Path+"/"+m.ID.String()), nil
}

func (h *monitorHandler) get(r *http.Request) (*plinth.Response, error) {
	id, err := plinth.PathUUID(r, "id")
	if err != nil {
		return nil, err
	}

	m, err := h.svc.Get(r.Context(), id)
	if err != nil {
		return nil, toAPIError(err)
	}

	return plinth.NewResponse(http.StatusOK, monitorFromDomain(m)), nil
}

func (h *monitorHandler) list(r *http.Request) (*plinth.Response, error) {
	limit, err := plinth.QueryInt(r, "limit", defaultListLimit)
	if err != nil {
		return nil, err
	}

	after, err := queryUUID(r, "after")
	if err != nil {
		return nil, err
	}

	if err := validateLimit(limit); err != nil {
		return nil, err
	}

	page, err := h.svc.List(r.Context(), after, int(limit))
	if err != nil {
		return nil, toAPIError(err)
	}

	return plinth.NewResponse(http.StatusOK, pageFromDomain(page)), nil
}

func (h *monitorHandler) update(r *http.Request) (*plinth.Response, error) {
	id, err := plinth.PathUUID(r, "id")
	if err != nil {
		return nil, err
	}

	req, err := parseRequest[updateMonitorRequest](r)
	if err != nil {
		return nil, err
	}

	m, err := h.svc.Update(r.Context(), id, req.toDomain())
	if err != nil {
		return nil, toAPIError(err)
	}

	return plinth.NewResponse(http.StatusOK, monitorFromDomain(m)), nil
}

func (h *monitorHandler) delete(r *http.Request) (*plinth.Response, error) {
	id, err := plinth.PathUUID(r, "id")
	if err != nil {
		return nil, err
	}

	if err := h.svc.Delete(r.Context(), id); err != nil {
		return nil, toAPIError(err)
	}

	return plinth.NewResponse(http.StatusNoContent, nil), nil
}

func parseRequest[T any](r *http.Request) (T, error) {
	return plinth.ParseRequestJSON[T](r, plinth.WithJSONOptions(json.RejectUnknownMembers(true)))
}
