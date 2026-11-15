package cloudlabel

import (
	"fmt"
	"os"
	"strings"

	"github.com/brian1917/illumiocloudapi"
	"github.com/brian1917/workloader/cmd/pcemgmt"
	"github.com/brian1917/workloader/utils"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Header values for the input CSV. Alternate header names are mapped in fieldMapping.
const (
	HeaderID          = "resource_id"
	HeaderName        = "resource_name"
	HeaderCloud       = "cloud"
	HeaderAccountID   = "account_id"
	HeaderAccountName = "account_name"
	HeaderObjectType  = "object_type"
	HeaderDestCspID   = "dest_csp_id"
	HeaderCategory    = "category"
	HeaderSubcategory = "subcategory"
	HeaderTags        = "tags"
	HeaderSegLicenses = "segmentation_licenses"
	HeaderInsLicenses = "insights_licenses"
	labelHeaderPrefix = "label:"
	defaultBatchSize  = 500
	matchOptionID     = "id"
	matchOptionName   = "name"
)

// Input is the data structure the ImportCloudLabelsFromCSV function expects
type Input struct {
	Tenant             illumiocloudapi.Tenant
	TenantName         string
	Cookie             string
	ImportFile         string
	ImportData         [][]string
	RemoveValue        string
	MatchString        string
	ObjectTypes        string
	Headers            map[string]int
	IgnoreCase         bool
	DoNotLogEachCSVRow bool
	UpdatePCE          bool
	NoPrompt           bool
	MaxUpdate          int
	BatchSize          int
}

// input is the cloud-label command's instance of Input
var input Input

func init() {
	CloudLabelCmd.Flags().StringVar(&input.TenantName, "tenant", "", "tenant name in the pce.yaml file.")
	CloudLabelCmd.Flags().StringVar(&input.Cookie, "cookie", "", "optionally set a cookie for authenticating to the api.")
	CloudLabelCmd.Flags().StringVar(&input.MatchString, "match", "", "match options. blank means to follow workloader default logic. available options are id and name. the default logic uses resource_id if present, then resource_name.")
	CloudLabelCmd.Flags().StringVar(&input.RemoveValue, "remove-value", "", "value in CSV used to remove existing labels. blank values in the CSV will not change existing. for example, to delete a label an option would be --remove-value DELETE and use DELETE in CSV to indicate where to clear existing labels on a resource.")
	CloudLabelCmd.Flags().BoolVar(&input.IgnoreCase, "ignore-case", false, "ignore case on the match string.")
	CloudLabelCmd.Flags().StringVar(&input.ObjectTypes, "resources", "", "optionally limit the cloud resources considered for matching to specific object types. object types should be comma-separated.")
	CloudLabelCmd.Flags().IntVar(&input.MaxUpdate, "max-update", -1, "maximum number of cloud resources that can be updated. -1 is unlimited.")
	CloudLabelCmd.Flags().BoolVar(&input.DoNotLogEachCSVRow, "do-not-log-each-csv-row", false, "do not log action for each csv row in workloader.log.")
	CloudLabelCmd.Flags().IntVar(&input.BatchSize, "batch-size", defaultBatchSize, "number of label assignments sent per api call.")
	CloudLabelCmd.Flags().MarkHidden("batch-size")
	CloudLabelCmd.Flags().SortFlags = false
}

// CloudLabelCmd runs the cloud-label command
var CloudLabelCmd = &cobra.Command{
	Use:   "cloud-label [csv file to import]",
	Short: "Assign labels to existing cloud resources from a CSV file.",
	Long: `
Assign labels to existing cloud resources from a CSV file.

The command only labels cloud resources that already exist in the cloud inventory. It never creates unmanaged workloads or any other objects. CSV rows that do not match an existing cloud resource are logged and skipped.

The input file requires headers and matches fields to header values. The output of cloud-inventory can be used as a starting point.

Match columns (one is required):
- ` + HeaderID + ` (also accepts id or csp_id)
- ` + HeaderName + ` (also accepts name)

Label columns:
Every header that is not one of the cloud-inventory headers below is treated as a label key. A header can also be explicitly marked as a label with the "label:" prefix (e.g., label:app).
Ignored cloud-inventory headers: ` + strings.Join([]string{HeaderCloud, HeaderAccountID, HeaderAccountName, HeaderObjectType, HeaderDestCspID, HeaderCategory, HeaderSubcategory, HeaderTags, HeaderSegLicenses, HeaderInsLicenses}, ", ") + `

Blank values in the CSV do not change existing labels. Use --remove-value to remove a label.

Recommended to run without --update-pce first to log what will change.`,

	Run: func(cmd *cobra.Command, args []string) {

		// Set the CSV file
		if len(args) != 1 {
			fmt.Println("Command requires 1 argument for the csv file. See usage help.")
			os.Exit(0)
		}
		input.ImportFile = args[0]

		// Get the update-pce and no-prompt values from viper
		input.UpdatePCE = viper.GetBool("update_pce")
		input.NoPrompt = viper.GetBool("no_prompt")

		// Get the cloud tenant and validate authentication information provided
		var err error
		input.Tenant, err = pcemgmt.GetTenantByName(input.TenantName)
		if err != nil {
			utils.LogErrorf("getting tenant by name - %s", err)
		}
		if input.Cookie != "" {
			input.Tenant.Cookie = input.Cookie
		}
		if input.Tenant.Cookie == "" && (input.Tenant.ClientID == "" || input.Tenant.Secret == "") {
			utils.LogErrorf("cannot authenticate to tenant - no client id and secret and no cookie")
		}

		ImportCloudLabelsFromCSV(input)
	},
}
