package logger

import (
	"bytes"
	"context"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/perfect-panel/backend/pkg/requestmeta"
	"github.com/stretchr/testify/assert"
)

func TestContextWithRequestMetadataPreservesAuthorizedRiskFields(t *testing.T) {
	var output bytes.Buffer
	oldWriter := Reset()
	SetWriter(NewWriter(&output))
	t.Cleanup(func() {
		Reset()
		if oldWriter != nil {
			SetWriter(oldWriter)
		}
	})

	ctx := ContextWithRequestMetadata(context.Background(), requestmeta.Metadata{
		ClientIP: "203.0.113.2", UserAgent: "RiskClient/2.0", ActorID: 8,
		IPMetadata: requestmeta.IPMetadata{
			IPCountryCode: "SG", IPCountry: "Singapore", IPRegion: "Central Singapore", IPCity: "Singapore",
			IPASN: 64500, IPASOrganization: "Example Network",
		},
	})
	WithContext(ctx).Info("request-scoped event")
	got := output.String()
	for _, expected := range []string{`"client_ip":"203.0.113.2"`, `"user_agent":"RiskClient/2.0"`, `"actor_id":8`, `"ip_country_code":"SG"`, `"ip_country":"Singapore"`, `"ip_region":"Central Singapore"`, `"ip_city":"Singapore"`, `"ip_asn":64500`, `"ip_as_organization":"Example Network"`} {
		if !strings.Contains(got, expected) {
			t.Fatalf("context log missing %s: %s", expected, got)
		}
	}
}

func TestContextWithFields(t *testing.T) {
	ctx := ContextWithFields(context.Background(), Field("a", 1), Field("b", 2))
	vals := ctx.Value(fieldsContextKey)
	assert.NotNil(t, vals)
	fields, ok := vals.([]LogField)
	assert.True(t, ok)
	assert.EqualValues(t, []LogField{Field("a", 1), Field("b", 2)}, fields)
}

func TestWithFieldsAppend(t *testing.T) {
	type ctxKey string
	var dummyKey ctxKey = "dummyKey"
	ctx := context.WithValue(context.Background(), dummyKey, "dummy")
	ctx = ContextWithFields(ctx, Field("a", 1), Field("b", 2))
	ctx = ContextWithFields(ctx, Field("c", 3), Field("d", 4))
	vals := ctx.Value(fieldsContextKey)
	assert.NotNil(t, vals)
	fields, ok := vals.([]LogField)
	assert.True(t, ok)
	assert.Equal(t, "dummy", ctx.Value(dummyKey))
	assert.EqualValues(t, []LogField{
		Field("a", 1),
		Field("b", 2),
		Field("c", 3),
		Field("d", 4),
	}, fields)
}

func TestWithFieldsAppendCopy(t *testing.T) {
	const count = 10
	ctx := context.Background()
	for i := 0; i < count; i++ {
		ctx = ContextWithFields(ctx, Field(strconv.Itoa(i), 1))
	}

	af := Field("foo", 1)
	bf := Field("bar", 2)
	ctxa := ContextWithFields(ctx, af)
	ctxb := ContextWithFields(ctx, bf)

	assert.EqualValues(t, af, ctxa.Value(fieldsContextKey).([]LogField)[count])
	assert.EqualValues(t, bf, ctxb.Value(fieldsContextKey).([]LogField)[count])
}

func BenchmarkAtomicValue(b *testing.B) {
	b.ReportAllocs()

	var container atomic.Value
	vals := []LogField{
		Field("a", "b"),
		Field("c", "d"),
		Field("e", "f"),
	}
	container.Store(&vals)

	for i := 0; i < b.N; i++ {
		val := container.Load()
		if val != nil {
			_ = *val.(*[]LogField)
		}
	}
}

func BenchmarkRWMutex(b *testing.B) {
	b.ReportAllocs()

	var lock sync.RWMutex
	vals := []LogField{
		Field("a", "b"),
		Field("c", "d"),
		Field("e", "f"),
	}

	for i := 0; i < b.N; i++ {
		lock.RLock()
		_ = vals
		lock.RUnlock()
	}
}
