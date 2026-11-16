package vsexport

import (
	"fmt"
	"strings"

	ia "github.com/brian1917/illumioapi/v2"
	"github.com/brian1917/workloader/utils"
	"github.com/spf13/cobra"
)

var noHref, active, removeDescNewLines bool
var outputFileName string

func init() {
	VsExportCmd.Flags().BoolVar(&noHref, "no-href", false, "do not export href column. use this when exporting data to import into different pce.")
	VsExportCmd.Flags().BoolVar(&active, "active", false, "export active (provisioned) virtual services instead of draft.")
	VsExportCmd.Flags().BoolVar(&removeDescNewLines, "remove-desc-newline", false, "will remove new line characters in description field.")
	VsExportCmd.Flags().StringVar(&outputFileName, "output-file", "", "optionally specify the name of the output file location. default is current location with a timestamped filename.")

	VsExportCmd.Flags().SortFlags = false
}

// VsExportCmd exports virtual services
var VsExportCmd = &cobra.Command{
	Use:     "vs-export",
	Aliases: []string{"virtualservice-export"},
	Short:   "Create a CSV export of all virtual services in the PCE.",
	Long: `
Create a CSV export of all virtual services in the PCE.

The default exports the draft version of virtual services. Use --active to export the provisioned version.

Multi-value fields are separated by semicolons:
- service_ports: "port proto", "port-toport proto", or "proto" (e.g., "443 tcp;8000-8010 udp;icmp").
- service_addresses: ip or fqdn with an optional "#description" (e.g., "10.0.0.5;app.example.com#primary").
- ip_overrides: ip addresses (e.g., "10.0.0.5;10.0.0.6").

The output can be used as input to vs-import. Use --no-href when importing into a different PCE.

The update-pce and --no-prompt flags are ignored for this command.`,
	Run: func(cmd *cobra.Command, args []string) {

		// Get the PCE.
		pce, err := utils.GetTargetPCEV2(false)
		if err != nil {
			utils.LogErrorf("getting pce - %s", err)
		}

		pStatus := "draft"
		if active {
			pStatus = "active"
		}

		// Load the pce
		apiResps, err := pce.Load(ia.LoadInput{ProvisionStatus: pStatus, VirtualServices: true, Services: true, LabelDimensions: true, Labels: true}, utils.UseMulti())
		utils.LogMultiAPIRespV2(apiResps)
		if err != nil {
			utils.LogErrorf("loading pce - %s", err)
		}

		csvData := CsvData(pce, !noHref, removeDescNewLines)
		if len(csvData) <= 1 {
			utils.LogInfo("no virtual services in PCE.", true)
			return
		}
		if outputFileName == "" {
			outputFileName = utils.FileName("")
		}
		utils.WriteOutput(csvData, csvData, outputFileName)
		utils.LogInfof(true, "%d virtual services exported", len(csvData)-1)
	},
}

// CsvData returns the virtual services loaded in the PCE as csv data.
// The PCE must have virtual services, services, labels, and label dimensions loaded.
func CsvData(pce ia.PCE, inclHref, removeDescNewLines bool) [][]string {

	// Get the label keys and determine if the label prefix is required.
	reserved := ReservedHeaders()
	labelKeys := []string{}
	labelHeaders := []string{}
	usePrefix := false
	for _, ld := range pce.LabelDimensionsSlice {
		labelKeys = append(labelKeys, ld.Key)
		if reserved[ld.Key] {
			usePrefix = true
		}
	}
	if usePrefix {
		utils.LogWarningf(true, "a label key matches a vs-export header. adding \"%s\" prefix to all label headers. this format is compatible with vs-import.", LabelPrefix)
	}
	for _, lk := range labelKeys {
		if usePrefix {
			labelHeaders = append(labelHeaders, LabelPrefix+lk)
		} else {
			labelHeaders = append(labelHeaders, lk)
		}
	}

	// Build the header row with labels after the description
	headers := []string{}
	for _, h := range AllHeaders(inclHref) {
		headers = append(headers, h)
		if h == HeaderDescription {
			headers = append(headers, labelHeaders...)
		}
	}
	csvData := [][]string{headers}

	for _, vs := range pce.VirtualServicesSlice {
		row := make(map[string]string)
		row[HeaderHref] = vs.Href
		row[HeaderName] = vs.Name
		row[HeaderDescription] = ia.PtrToVal(vs.Description)
		if removeDescNewLines {
			row[HeaderDescription] = utils.ReplaceNewLine(row[HeaderDescription])
		}
		if vs.Service != nil && vs.Service.Href != "" {
			row[HeaderService] = pce.Services[vs.Service.Href].Name
			if row[HeaderService] == "" {
				utils.LogWarningf(false, "%s - service %s not found in pce services", vs.Name, vs.Service.Href)
				row[HeaderService] = vs.Service.Href
			}
		}
		row[HeaderServicePorts] = strings.Join(ServicePortsToString(vs.ServicePorts), ";")
		row[HeaderServiceAddresses] = strings.Join(ServiceAddressesToString(vs.ServiceAddresses), ";")
		row[HeaderIPOverrides] = strings.Join(ia.PtrToVal(vs.IPOverrides), ";")
		row[HeaderApplyTo] = vs.ApplyTo
		row[HeaderExternalDataSet] = vs.ExternalDataSet
		row[HeaderExternalDataReference] = vs.ExternalDataReference
		row[HeaderUpdateType] = vs.UpdateType
		row[HeaderCreatedAt] = vs.CreatedAt
		row[HeaderUpdatedAt] = vs.UpdatedAt
		for i, lk := range labelKeys {
			row[labelHeaders[i]] = vs.GetLabelByKey(lk, pce.Labels).Value
		}

		csvRow := []string{}
		for _, h := range headers {
			csvRow = append(csvRow, row[h])
		}
		csvData = append(csvData, csvRow)
	}

	if len(csvData) > 1 {
		utils.LogInfo(fmt.Sprintf("%d virtual services processed for export", len(csvData)-1), false)
	}

	return csvData
}
