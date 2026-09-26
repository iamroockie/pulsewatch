package rest

//go:generate mockgen -source=monitor.go -destination=mock_service_test.go -package=rest_test

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"uuid"

	"github.com/iamroockie/plinth"

	"github.com/iamroockie/pulsewatch/internal/domain"
	"github.com/iamroockie/pulsewatch/internal/service"
)

const (
	defaultListLimit = 50
	minListLimit     = 1
	maxListLimit     = 100
)

type MonitorService interface {
	Create(ctx context.Context, settings domain.CheckSettings) (*domain.Monitor, error)
	Get(ctx context.Context, id uuid.UUID) (*domain.Monitor, error)
	List(ctx context.Context, after uuid.UUID, limit int) (service.MonitorPage, error)
	Delete(ctx context.Context, id uuid.UUID) error
	Update(
		ctx context.Context,
		id uuid.UUID,
		changes domain.MonitorChanges,
	) (*domain.Monitor, error)
}

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

	if limit < minListLimit || limit > maxListLimit {
		return nil, plinth.ValidationError(plinth.FieldViolation{
			Field:  "limit",
			Code:   codeOutOfRange,
			Params: rangeParams(minListLimit, maxListLimit),
		})
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

func queryUUID(r *http.Request, name string) (uuid.UUID, error) {
	value := r.URL.Query().Get(name)
	if value == "" {
		return uuid.Nil(), nil
	}

	id, err := uuid.Parse(value)
	if err != nil {
		msg := fmt.Sprintf("invalid query parameter %q", name)

		return uuid.Nil(), plinth.BadRequestError(msg, err)
	}

	return id, nil
}
