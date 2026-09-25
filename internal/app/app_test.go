package app

import (
	"net/http"
	"testing"

	"github.com/iamroockie/plinth/plinthtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthz(t *testing.T) {
	req := plinthtest.NewRequest(t, http.MethodGet, "/", nil)

	rec, err := plinthtest.Serve(t, healthz(), req)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	got := plinthtest.DecodeJSON[map[string]string](t, rec)
	assert.Equal(t, "ok", got["status"])
}
