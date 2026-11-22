package auditlog

import (
	"reflect"

	"github.com/perfect-panel/backend/pkg/requestmeta"
)

// requestMetadataFields are the request fields of the log DTOs, named as in
// requestmeta.Metadata. The login and register logs record the address
// under their own name (LoginIP, RegisterIP) and carry the other eight.
var requestMetadataFields = [...]string{
	"ClientIP", "UserAgent", "ActorID",
	"IPCountryCode", "IPCountry", "IPRegion", "IPCity", "IPASN", "IPASOrganization",
}

// withRequestMetadata copies the request metadata of a log entry into the
// request fields of the log DTO item points to, and returns the DTO. A field
// the DTO lacks is left out.
func withRequestMetadata[T any](item *T, metadata requestmeta.Metadata) T {
	src := reflect.ValueOf(metadata)
	dst := reflect.ValueOf(item).Elem()
	for _, name := range requestMetadataFields {
		if field := dst.FieldByName(name); field.IsValid() && field.CanSet() {
			field.Set(src.FieldByName(name))
		}
	}
	return *item
}
