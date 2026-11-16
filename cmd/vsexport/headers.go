package vsexport

const (
	HeaderHref                  = "href"
	HeaderName                  = "name"
	HeaderDescription           = "description"
	HeaderService               = "service"
	HeaderServicePorts          = "service_ports"
	HeaderServiceAddresses      = "service_addresses"
	HeaderIPOverrides           = "ip_overrides"
	HeaderApplyTo               = "apply_to"
	HeaderExternalDataSet       = "external_data_set"
	HeaderExternalDataReference = "external_data_reference"
	HeaderUpdateType            = "update_type"
	HeaderCreatedAt             = "created_at"
	HeaderUpdatedAt             = "updated_at"
)

// LabelPrefix can be used in a header to designate a label key (e.g., label:app).
// It is required when a label key matches one of the reserved headers.
const LabelPrefix = "label:"

// AllHeaders returns the non-label headers in export order.
func AllHeaders(inclHref bool) []string {
	headers := []string{}
	if inclHref {
		headers = append(headers, HeaderHref)
	}
	return append(headers,
		HeaderName,
		HeaderDescription,
		HeaderService,
		HeaderServicePorts,
		HeaderServiceAddresses,
		HeaderIPOverrides,
		HeaderApplyTo,
		HeaderExternalDataSet,
		HeaderExternalDataReference,
		HeaderUpdateType,
		HeaderCreatedAt,
		HeaderUpdatedAt,
	)
}

// ImportHeaders returns the non-label headers vs-import processes.
func ImportHeaders() []string {
	return []string{
		HeaderHref,
		HeaderName,
		HeaderDescription,
		HeaderService,
		HeaderServicePorts,
		HeaderServiceAddresses,
		HeaderIPOverrides,
		HeaderApplyTo,
		HeaderExternalDataSet,
		HeaderExternalDataReference,
	}
}

// ReservedHeaders returns a map of all headers that cannot be used as un-prefixed label keys.
func ReservedHeaders() map[string]bool {
	m := make(map[string]bool)
	for _, h := range AllHeaders(true) {
		m[h] = true
	}
	return m
}
