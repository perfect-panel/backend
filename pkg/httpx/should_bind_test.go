package httpx

import (
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/stretchr/testify/require"
)

func TestShouldBind_bindsJSON_whenContentTypeIsJSON(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
	}{
		{name: "application json", contentType: "application/json; charset=utf-8"},
		{name: "structured json", contentType: "application/vnd.perfect-panel+json"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// Given
			requestContext := newRequestContext("/resources?name=query", test.contentType, `{"name":"body"}`)
			var destination jsonDestination

			// When
			err := ShouldBind(requestContext, &destination)

			// Then
			require.NoError(t, err)
			require.Equal(t, jsonDestination{Name: "body"}, destination)
		})
	}
}

func TestShouldBind_bindsQueryBeforeBody_whenNonJSONBodyPresent(t *testing.T) {
	// Given
	requestContext := newRequestContext("/resources?query_only=query&name=query", "application/x-www-form-urlencoded", "name=body")
	var destination formDestination

	// When
	err := ShouldBind(requestContext, &destination)

	// Then
	require.NoError(t, err)
	require.Equal(t, formDestination{QueryOnly: "query", Name: "body"}, destination)
}

func TestShouldBind_bindsQuery_whenNonJSONBodyIsEmpty(t *testing.T) {
	// Given
	requestContext := newRequestContext("/resources?name=query", "application/x-www-form-urlencoded", "")
	var destination formDestination

	// When
	err := ShouldBind(requestContext, &destination)

	// Then
	require.NoError(t, err)
	require.Equal(t, formDestination{Name: "query"}, destination)
}

func TestShouldBind_bindsCompatibilityTagsAndFields_whenQueryUsesThem(t *testing.T) {
	// Given
	uri := "/resources?form_name=form&query_name=query&uri_name=uri&path_name=path&json_name=json&embedded=embedded&pointer=7&label=first&label=second&id=1&id=2&name%5B%5D=alice&name%5B%5D=bob"
	requestContext := newRequestContext(uri, "application/x-www-form-urlencoded", "")
	var destination compatibilityDestination

	// When
	err := ShouldBind(requestContext, &destination)

	// Then
	require.NoError(t, err)
	require.Equal(t, compatibilityDestination{
		embeddedDestination: embeddedDestination{Value: "embedded"},
		Form:                "form",
		Query:               "query",
		URI:                 "uri",
		Path:                "path",
		JSON:                "json",
		Pointer:             pointerTo(7),
		Labels:              []string{"first", "second"},
		IDs:                 []int{1, 2},
		Names:               []string{"alice", "bob"},
	}, destination)
}

type jsonDestination struct {
	Name string `json:"name"`
}

type formDestination struct {
	QueryOnly string `form:"query_only"`
	Name      string `form:"name"`
}

type embeddedDestination struct {
	Value string `form:"embedded"`
}

type compatibilityDestination struct {
	embeddedDestination
	Form    string   `form:"form_name" query:"query_name" json:"form_json_name"`
	Query   string   `query:"query_name"`
	URI     string   `uri:"uri_name"`
	Path    string   `path:"path_name"`
	JSON    string   `json:"json_name"`
	Pointer *int     `form:"pointer"`
	Labels  []string `form:"label"`
	IDs     []int    `form:"id"`
	Names   []string `form:"name"`
}

func newRequestContext(requestURI, contentType, body string) *app.RequestContext {
	ctx := app.NewContext(0)
	ctx.Request.SetRequestURI(requestURI)
	ctx.Request.Header.Set("Content-Type", contentType)
	ctx.Request.SetBodyString(body)
	return ctx
}

func pointerTo(value int) *int {
	return &value
}
