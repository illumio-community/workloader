package cloudlabel

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/brian1917/workloader/utils"
)

// fieldMapping maps alternate header names to the standard header names
func fieldMapping() map[string]string {
	return map[string]string{
		"id":              HeaderID,
		"csp_id":          HeaderID,
		HeaderID:          HeaderID,
		"name":            HeaderName,
		HeaderName:        HeaderName,
		HeaderCloud:       HeaderCloud,
		HeaderAccountID:   HeaderAccountID,
		HeaderAccountName: HeaderAccountName,
		HeaderObjectType:  HeaderObjectType,
		HeaderDestCspID:   HeaderDestCspID,
		HeaderCategory:    HeaderCategory,
		HeaderSubcategory: HeaderSubcategory,
		HeaderTags:        HeaderTags,
		HeaderSegLicenses: HeaderSegLicenses,
		HeaderInsLicenses: HeaderInsLicenses,
	}
}

// ProcessHeaders maps the CSV headers, determines label columns, and sets the match column.
// It returns a map of label key to column index.
func (i *Input) ProcessHeaders(headers []string) (labelCols map[string]int) {

	fieldMap := fieldMapping()
	i.Headers = make(map[string]int)
	labelCols = make(map[string]int)

	for col, header := range headers {
		header = strings.TrimSpace(header)
		if header == "" {
			continue
		}
		// Explicit label header
		if strings.HasPrefix(header, labelHeaderPrefix) {
			key := strings.TrimPrefix(header, labelHeaderPrefix)
			if key != "" {
				labelCols[key] = col
			}
			continue
		}
		// Known non-label header
		if std, ok := fieldMap[strings.ToLower(header)]; ok {
			i.Headers[std] = col
			continue
		}
		// Everything else is a label key
		labelCols[header] = col
	}

	// Validate or set the match column
	switch i.MatchString {
	case matchOptionID:
		i.MatchString = HeaderID
	case matchOptionName:
		i.MatchString = HeaderName
	case "":
		if _, ok := i.Headers[HeaderID]; ok {
			i.MatchString = HeaderID
		} else if _, ok := i.Headers[HeaderName]; ok {
			i.MatchString = HeaderName
		} else {
			utils.LogError("cannot set a match column. csv requires a resource_id or resource_name column.")
		}
	default:
		utils.LogError("invalid match value. must be id or name")
	}
	if _, ok := i.Headers[i.MatchString]; !ok {
		utils.LogErrorf("match column %s is not in the csv", i.MatchString)
	}
	utils.LogInfof(false, "match column set to %s (%d)", i.MatchString, i.Headers[i.MatchString])

	if len(labelCols) == 0 {
		utils.LogError("no label columns found in the csv")
	}
	utils.LogInfof(false, "label columns: %v", labelCols)

	return labelCols
}

func (i *Input) log() {
	v := reflect.ValueOf(*i)
	logEntry := []string{}
	for a := 0; a < v.NumField(); a++ {
		name := v.Type().Field(a).Name
		if name == "Tenant" || name == "ImportData" || name == "Cookie" {
			continue
		}
		logEntry = append(logEntry, fmt.Sprintf("%s: %v", name, v.Field(a).Interface()))
	}
	utils.LogInfo(strings.Join(logEntry, "; "), false)
}

// LabelHeader returns the CSV header cloud-label expects for a label key.
// Keys that collide with a reserved header (or already start with the label: prefix) are prefixed with label: so they are still read as labels.
func LabelHeader(key string) string {
	if _, ok := fieldMapping()[strings.ToLower(strings.TrimSpace(key))]; ok || strings.HasPrefix(key, labelHeaderPrefix) {
		return labelHeaderPrefix + key
	}
	return key
}
