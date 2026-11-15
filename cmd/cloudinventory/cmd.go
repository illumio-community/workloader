package cloudinventory

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/brian1917/illumiocloudapi"
	"github.com/brian1917/workloader/cmd/cloudlabel"
	"github.com/brian1917/workloader/cmd/pcemgmt"
	"github.com/brian1917/workloader/utils"
	"github.com/spf13/cobra"
)

// Declare local global variables
var tenantName, outputFileName, cloudCookie, resources string
var includeLicenses bool

func init() {
	CloudInventoryCmd.Flags().StringVar(&tenantName, "tenant", "", "tenant name in the pce.yaml file.")
	CloudInventoryCmd.Flags().StringVar(&outputFileName, "output-file", "", "optionally specify the name of the output file location. default is current location with a timestamped filename.")
	CloudInventoryCmd.Flags().BoolVarP(&includeLicenses, "licenses", "l", false, "include license information in the output.")
	CloudInventoryCmd.Flags().StringVar(&cloudCookie, "cookie", "", "optionally set a cookie for authenticating to the api.")
	CloudInventoryCmd.Flags().StringVar(&resources, "resources", "", "optionally specify only specific resources to include in the export. resources should be comma-separated. do not use spaces after commas.")
	CloudInventoryCmd.Flags().SortFlags = false
}

// CloudInventoryCmd runs the label-export command
var CloudInventoryCmd = &cobra.Command{
	Use:   "cloud-inventory",
	Short: "Create a CSV export of the cloud inventory.",
	Long: `
Create a CSV export of the cloud inventory.

The update-pce and --no-prompt flags are ignored for this command.`,
	Run: func(cmd *cobra.Command, args []string) {

		CloudInventory()
	},
}

func CloudInventory() {

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

	// Parse the resources flag into a slice
	resourceList := []string{}
	if resources != "" {
		resourceList = strings.Split(resources, ",")
		for i := range resourceList {
			resourceList[i] = strings.TrimSpace(resourceList[i])
		}
	}

	// apiResponses, err := tenant.GetResources(illumiocloudapi.ResourcesPostRequest{})
	apiResponses, err := tenant.GetResources(illumiocloudapi.ResourcesPostRequest{ObjectType: resourceList})
	utils.LogMultiAPIRespV2(apiResponses)
	if err != nil {
		utils.LogErrorf("getting resources from cloud - %s", err)
	}

	// If we have no resources, exit
	if len(tenant.Resources) == 0 {
		utils.LogInfo("no resources found.", true)
		return
	}

	// If licensing option is set, get the licenses info
	var segmentationLicenseResourceMap, insightsLicenseResourceMap map[string]float64
	var segmentationLicenseSummaryMap, insightsLicenseSummaryMap map[string]float64
	var resourceToCloudMaps map[string]string
	allUniqueLicensedResourceTypes := make(map[string]bool)

	if includeLicenses {
		segmentationLicenseResourceMap, insightsLicenseResourceMap, resourceToCloudMaps = getMappings()
		segmentationLicenseSummaryMap = make(map[string]float64)
		insightsLicenseSummaryMap = make(map[string]float64)
		for objectType := range insightsLicenseResourceMap {
			allUniqueLicensedResourceTypes[objectType] = true
		}
		for objectType := range segmentationLicenseResourceMap {
			allUniqueLicensedResourceTypes[objectType] = true
		}
	}

	// Output the data to a csv file
	// Header names come from cloudlabel so this output can be used as cloud-label input
	csvData := [][]string{{cloudlabel.HeaderCloud, cloudlabel.HeaderAccountID, cloudlabel.HeaderAccountName, cloudlabel.HeaderID, cloudlabel.HeaderName, cloudlabel.HeaderObjectType, cloudlabel.HeaderDestCspID, cloudlabel.HeaderCategory, cloudlabel.HeaderSubcategory, cloudlabel.HeaderTags}}
	if includeLicenses {
		csvData[0] = append(csvData[0], cloudlabel.HeaderSegLicenses, cloudlabel.HeaderInsLicenses)
	}

	// Get every label key used across the resources and add a column for each
	labelKeyMap := make(map[string]bool)
	for _, item := range tenant.Resources {
		for _, l := range item.Labels {
			labelKeyMap[l.Key] = true
		}
	}
	labelKeys := make([]string, 0, len(labelKeyMap))
	for k := range labelKeyMap {
		labelKeys = append(labelKeys, k)
	}
	sort.Strings(labelKeys)
	for _, k := range labelKeys {
		csvData[0] = append(csvData[0], cloudlabel.LabelHeader(k))
	}

	// Add the resource data
	for _, item := range tenant.Resources {
		x := ""

		if item.Properties.DestinationCSPID != "" {
			x = item.Properties.DestinationCSPID
		}
		// build tags string
		tagKeys := make([]string, 0, len(item.Tags))
		for k := range item.Tags {
			tagKeys = append(tagKeys, k)
		}
		sort.Strings(tagKeys)
		tagPairs := make([]string, 0, len(tagKeys))
		for _, k := range tagKeys {
			tagPairs = append(tagPairs, k+"="+item.Tags[k])
		}
		tagsStr := strings.Join(tagPairs, ";")
		row := []string{
			item.Cloud,
			item.AccountID,
			item.AccountName,
			item.Id,
			item.Name,
			item.ObjectType,
			x,
			item.Category,
			item.Subcategory,
			tagsStr,
		}
		if includeLicenses {
			row = append(row, strconv.FormatFloat(segmentationLicenseResourceMap[item.ObjectType], 'f', -1, 64), strconv.FormatFloat(insightsLicenseResourceMap[item.ObjectType], 'f', -1, 64))
			segmentationLicenseSummaryMap[item.ObjectType] = segmentationLicenseSummaryMap[item.ObjectType] + segmentationLicenseResourceMap[item.ObjectType]
			insightsLicenseSummaryMap[item.ObjectType] = insightsLicenseSummaryMap[item.ObjectType] + insightsLicenseResourceMap[item.ObjectType]
		}
		for _, k := range labelKeys {
			row = append(row, item.GetLabelValueByKey(k))
		}
		csvData = append(csvData, row)
	}

	if outputFileName == "" {
		outputFileName = utils.FileName("")
	}
	utils.WriteOutput(csvData, nil, outputFileName)
	utils.LogInfof(true, "%d cloud resources exported.", len(csvData)-1)

	// Create the summary license files
	if includeLicenses {
		csvData = [][]string{{"cloud", "object_type", "segmentation_ratio", "segmentation_licenses", "insights_ratio", "insights_licenses"}}
		for objectType := range allUniqueLicensedResourceTypes {
			// Skip zero counts
			if segmentationLicenseSummaryMap[objectType]+insightsLicenseSummaryMap[objectType] == 0 {
				continue
			}
			row := []string{
				resourceToCloudMaps[objectType],
				objectType,
				strconv.FormatFloat(segmentationLicenseResourceMap[objectType], 'f', -1, 64),
				strconv.FormatFloat(segmentationLicenseSummaryMap[objectType], 'f', 1, 64),
				strconv.FormatFloat(insightsLicenseResourceMap[objectType], 'f', -1, 64),
				strconv.FormatFloat(insightsLicenseSummaryMap[objectType], 'f', 1, 64),
			}
			csvData = append(csvData, row)
		}
		summaryFileName := fmt.Sprintf("%s-license-summary.csv", outputFileName[0:len(outputFileName)-4])
		utils.WriteOutput(csvData, nil, summaryFileName)
		utils.LogInfof(true, "license summary exported to %s", summaryFileName)
	}
}
