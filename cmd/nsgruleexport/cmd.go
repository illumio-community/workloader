package nsgruleexport

import (
	"sort"
	"strconv"
	"strings"

	"github.com/brian1917/illumiocloudapi"
	"github.com/brian1917/workloader/cmd/pcemgmt"
	"github.com/brian1917/workloader/utils"
	"github.com/spf13/cobra"
)

// Declare local global variables
var tenantName, outputFileName, cloudCookie string

func init() {
	CloudInventoryCmd.Flags().StringVar(&tenantName, "tenant", "", "tenant name in the pce.yaml file.")
	CloudInventoryCmd.Flags().StringVar(&outputFileName, "output-file", "", "optionally specify the name of the output file location. default is current location with a timestamped filename.")
	CloudInventoryCmd.Flags().StringVar(&cloudCookie, "cookie", "", "optionally set a cookie for authenticating to the api.")
	CloudInventoryCmd.Flags().SortFlags = false
}

// CloudInventoryCmd runs the label-export command
var CloudInventoryCmd = &cobra.Command{
	Use:   "nsg-rule-export",
	Short: "Create a CSV of all nsg rules.",
	Long: `
Create a CSV export of all nsg rules.

The update-pce and --no-prompt flags are ignored for this command.`,
	Run: func(cmd *cobra.Command, args []string) {

		NsgRuleExport()
	},
}

func NsgRuleExport() {
	// Get the cloud tenant and validate authentication information provided
	tenant, err := pcemgmt.GetTenantByName(tenantName)
	if err != nil {
		utils.LogErrorf("getting tenant by name - %s", err)
	}
	if cloudCookie != "" {
		tenant.Cookie = cloudCookie
	}
	if tenant.Cookie == "" && (tenant.ClientID == "" || tenant.Secret == "") {
		utils.LogErrorf("cannot authenticate to tenant - no client id and secret and no cookie)")
	}

	// Get the NSGs
	apiResponses, err := tenant.GetResources(illumiocloudapi.ResourcesPostRequest{ObjectType: []string{"Microsoft.Network/networkSecurityGroups"}})
	utils.LogMultiAPIRespV2(apiResponses)
	if err != nil {
		utils.LogErrorf("getting nsgs - %s", err)
	}

	// If we have no resources, exit
	if len(tenant.Resources) == 0 {
		utils.LogInfo("no nsgs found.", true)
		return
	}

	// Start the CSV data with headers
	csvData := [][]string{{"nsg_name", "nsg_id", "nsg_tags", "contains_illumio_rules_tag", "illumio_managed_rule", "rule_name", "priority", "protocol", "source", "src_ports", "destination", "dst_ports", "action", "num_associated_policies"}}

	// Iterate through each NSG and get the rules
	for _, item := range tenant.Resources {
		// Build tags string
		containsIllumioRulesTag := false
		tagKeys := make([]string, 0, len(item.Tags))
		for k, v := range item.Tags {
			tagKeys = append(tagKeys, k)
			// Check for the allow-illumio-rules tag
			if k == "allow-illumio-rules" && strings.ToLower(v) == "true" {
				containsIllumioRulesTag = true
			}
		}
		sort.Strings(tagKeys)
		tagPairs := make([]string, 0, len(tagKeys))
		for _, k := range tagKeys {
			tagPairs = append(tagPairs, k+"="+item.Tags[k])
		}
		tagsStr := strings.Join(tagPairs, ";")

		// Get the NSG rules
		nsgRules, api, err := tenant.GetNsgRules(item.Id)
		utils.LogAPIRespV2("GetNsgRules", api)
		if err != nil {
			utils.LogErrorf("getting nsg rules - %s", err)
		}
		for _, rule := range nsgRules.Rules {
			// Check if the rule is illumio managed
			illumioManagedRule := false
			if strings.HasPrefix(rule.AzureNsg.RuleName, "ICS") {
				illumioManagedRule = true
			}
			csvData = append(csvData, []string{
				item.Name,
				item.Id,
				tagsStr,
				strconv.FormatBool(containsIllumioRulesTag),
				strconv.FormatBool(illumioManagedRule),
				rule.AzureNsg.RuleName,
				strconv.Itoa(rule.AzureNsg.Priority),
				strconv.Itoa(rule.AzureNsg.Protocol.Protocol),
				actorsOrType(rule.AzureNsg.Source.AllActors(), rule.AzureNsg.Source.Type),
				joinPorts(rule.AzureNsg.SrcPorts.PortRange),
				actorsOrType(rule.AzureNsg.Destination.AllActors(), rule.AzureNsg.Destination.Type),
				joinPorts(rule.AzureNsg.DstPorts.PortRange),
				rule.AzureNsg.Action,
				strconv.Itoa(rule.NumAssociatedPolicies),
			})
		}
	}

	// Write the output
	if len(csvData) == 1 {
		utils.LogInfo("no nsg rules found.", true)
		return
	}
	if outputFileName == "" {
		outputFileName = utils.FileName("")
	}
	utils.WriteOutput(csvData, nil, outputFileName)
	utils.LogInfof(true, "%d nsg rules exported.", len(csvData)-1)
}

// joinPorts converts a list of port ranges to a semicolon-separated string
func joinPorts(portRanges []illumiocloudapi.PortRange) string {
	ports := make([]string, 0, len(portRanges))
	for _, pr := range portRanges {
		ports = append(ports, strconv.Itoa(pr.Port))
	}
	return strings.Join(ports, ";")
}

// actorsOrType returns the actors as a semicolon-separated string, falling back to the type when there are no actors
func actorsOrType(actors []string, actorType string) string {
	if len(actors) == 0 {
		return actorType
	}
	return strings.Join(actors, ";")
}
