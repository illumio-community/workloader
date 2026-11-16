package vsimport

import (
	"fmt"
	"os"

	"github.com/brian1917/workloader/cmd/vsexport"
	"github.com/brian1917/workloader/utils"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var input Input

func init() {
	VsImportCmd.Flags().BoolVar(&input.Create, "create", true, "create virtual services that do not exist in the PCE. --create=false will only update existing virtual services.")
	VsImportCmd.Flags().BoolVar(&input.Update, "update", true, "update existing virtual services. --update=false will only create new virtual services.")
	VsImportCmd.Flags().StringVar(&input.RemoveValue, "remove-value", "", "value in CSV used to clear existing values. Blank values in the CSV will not change existing. for example, use --remove-value DELETE and put DELETE in the CSV to clear a label, description, service_addresses, or ip_overrides.")
	VsImportCmd.Flags().BoolVar(&input.IgnoreHref, "ignore-href", false, "ignore the href column in the CSV and match on name. useful when importing a CSV exported from a different PCE.")
	VsImportCmd.Flags().BoolVarP(&input.Provision, "provision", "p", false, "provision created and updated virtual services.")

	VsImportCmd.Flags().SortFlags = false
}

// VsImportCmd runs the virtual service import
var VsImportCmd = &cobra.Command{
	Use:   "vs-import [csv file to import]",
	Short: "Create and update virtual services from a CSV file.",
	Long: `
Create and update virtual services from a CSV file.

The input file requires headers and matches fields to header values. Column headers that are not label keys or in the list below are ignored:
` + "\r\n- " + vsexport.HeaderHref + "\r\n" +
		"- " + vsexport.HeaderName + " (required to create)\r\n" +
		"- " + vsexport.HeaderDescription + "\r\n" +
		"- " + vsexport.HeaderService + " (name of an existing service object)\r\n" +
		"- " + vsexport.HeaderServicePorts + "\r\n" +
		"- " + vsexport.HeaderServiceAddresses + "\r\n" +
		"- " + vsexport.HeaderIPOverrides + "\r\n" +
		"- " + vsexport.HeaderApplyTo + " (host_only or internal_bridge_network)\r\n" +
		"- " + vsexport.HeaderExternalDataSet + "\r\n" +
		"- " + vsexport.HeaderExternalDataReference + "\r\n" + `
Label keys can be used as headers directly (e.g., app) or with the "label:" prefix (e.g., label:app). The prefix is required if a label key matches one of the headers above. Labels that do not exist are created.

A virtual service uses either a service object (service column) or service ports (service_ports column), not both. One is required to create a virtual service.

Multi-value fields are separated by semicolons. A non-blank value replaces all existing entries:
- service_ports: "port proto", "port-toport proto", or "proto" (e.g., "443 tcp;8000-8010 udp;icmp").
- service_addresses: ip or fqdn with an optional "#description" (e.g., "10.0.0.5;app.example.com#primary").
- ip_overrides: ip addresses (e.g., "10.0.0.5;10.0.0.6").

Matching: if the href column is present and populated (and --ignore-href is not set), the virtual service is matched on href. Otherwise it is matched on name. If no match is found by name, the virtual service is created.

Blank values in the CSV do not change existing values. Use --remove-value to clear values.

Changes are made to the draft policy. Use --provision to provision the created and updated virtual services.

Recommended to run without --update-pce first to log what will change.`,
	Run: func(cmd *cobra.Command, args []string) {

		var err error
		input.PCE, err = utils.GetTargetPCEV2(true)
		if err != nil {
			utils.LogErrorf("getting pce - %s", err)
		}

		if len(args) != 1 {
			fmt.Println("command requires 1 argument for the csv file. see usage help.")
			os.Exit(0)
		}
		input.ImportFile = args[0]

		input.UpdatePCE = viper.GetBool("update_pce")
		input.NoPrompt = viper.GetBool("no_prompt")

		ImportVirtualServicesFromCSV(input)
	},
}
