package ruliu

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClientSendBuildsAlertMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			t.Errorf("method = %s, want %s", request.Method, http.MethodPost)
		}
		if contentType := request.Header.Get("Content-Type"); contentType != "application/json" {
			t.Errorf("content type = %s, want application/json", contentType)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"errcode":0,"data":{"fail":{}}}`))
	}))
	defer server.Close()

	err := NewClient(server.Client()).Send(context.Background(), AlertParams{
		AlertURL:  server.URL,
		AlertToID: []int{1001},
		Msg:       "error summary",
		Link:      "https://example.com/log",
		AtUserIDs: []string{"u001"},
	})
	require.NoError(t, err)
}

func TestClientSendRejectsFailedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"errcode":1,"errmsg":"failed","data":{"fail":{}}}`))
	}))
	defer server.Close()

	err := NewClient(server.Client()).Send(context.Background(), AlertParams{
		AlertURL:  server.URL,
		AlertToID: []int{1001},
		Msg:       "error summary",
	})
	require.Error(t, err)
}
