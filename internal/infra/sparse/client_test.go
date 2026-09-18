package sparse

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type mockTransport struct{ body string }

func (m *mockTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	var req sparseRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.Model != "bge-m3" {
		return nil, &errMock{msg: "bad model"}
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(m.body)),
	}, nil
}

type errMock struct{ msg string }

func (e *errMock) Error() string { return e.msg }

func TestCreateSparse(t *testing.T) {
	c := NewClient("http://mock", "bge-m3", 5).(*httpClient)
	c.http = &http.Client{Transport: &mockTransport{body: `{"data":[{"tokens":["斑马","RAG"],"values":[0.9,0.8]}]}`}}

	sv, err := c.CreateSparse(context.Background(), "Zebra RAG知识库")
	if err != nil {
		t.Fatalf("CreateSparse error: %v", err)
	}
	if sv["斑马"] != 0.9 || sv["RAG"] != 0.8 {
		t.Errorf("unexpected sparse vector: %v", sv)
	}
}
