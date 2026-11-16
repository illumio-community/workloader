package unusedumwl

import (
	"time"

	"github.com/brian1917/illumioapi"
	"github.com/brian1917/workloader/utils"
	"github.com/spf13/cobra"
)

var pce illumioapi.PCE
var err error
var start, end, exclServiceCSV, outputFileName, labelFile, hrefFile string
var nonUni, includeAllUmwls bool
var maxResults int

func init() {
	UnusedUmwlCmd.Flags().StringVar(&labelFile, "label-file", "", "csv file with labels to scope the unmanaged workloads checked. see description below.")
	UnusedUmwlCmd.Flags().StringVar(&hrefFile, "href-file", "", "csv file with unmanaged workload hrefs to check. uses the column with an \"href\" header if present, otherwise the first column. wkld-export output can be used directly.")
	UnusedUmwlCmd.Flags().BoolVarP(&includeAllUmwls, "all", "a", false, "export all umwls with traffic count. default only exports umwl with 0 traffic.")
	UnusedUmwlCmd.Flags().IntVarP(&maxResults, "max-results", "m", 1000, "max results in explorer. Maximum value is 100000. A higher maxiumum value is ")
	UnusedUmwlCmd.Flags().StringVarP(&start, "start", "s", time.Now().AddDate(0, 0, -88).In(time.UTC).Format("2006-01-02"), "start date in the format of yyyy-mm-dd.")
	UnusedUmwlCmd.Flags().StringVarP(&end, "end", "e", time.Now().Add(time.Hour*24).Format("2006-01-02"), "end date in the format of yyyy-mm-dd.")
	UnusedUmwlCmd.Flags().BoolVarP(&nonUni, "incl-non-unicast", "n", false, "includes non-unicast (broadcast and multicast) flows in the output. Default is unicast only.")
	UnusedUmwlCmd.Flags().StringVarP(&exclServiceCSV, "excl-svc-file", "x", "", "file location of csv with port/protocols to exclude. Port number in column 1 and IANA numeric protocol in Col 2. Headers optional.")
	UnusedUmwlCmd.Flags().StringVar(&outputFileName, "output-file", "", "optionally specify the name of the output file location. default is current location with a timestamped filename. If iterating through labels, the labels will be appended to the provided name before the provided file extension. To name the files for the labels, use just an extension (--output-file .csv).")
	UnusedUmwlCmd.Flags().SortFlags = false

}

// UnusedUmwlCmd produces a report of unmanaged workloads that do not have traffic to them
var UnusedUmwlCmd = &cobra.Command{
	Use:   "unused-umwl",
	Short: "Create a report of unmanaged workloads with no traffic.",
	Long: `
	Create a report of unmanaged workloads with no traffic.

By default, all unmanaged workloads are checked. Use --label-file or --href-file (not both) to limit the check to a subset.

The label-file's first row should be label keys. The workload query uses an AND operator for entries on the same row and an OR operator for the separate rows. An example label file is below:
+------+-----+----------------+-----+
| role | app | env            | loc |
+------+-----+----------------+-----+
|      |     | decommissioned |     |
| web  | erp |                |     |
+------+-----+----------------+-----+
This example checks all unmanaged workloads that are
- decommissioned (env)
- OR web (role) AND erp (app)

The update-pce and --no-prompt flags are ignored for this command.`,
	Run: func(cmd *cobra.Command, args []string) {

		pce, err = utils.GetTargetPCE(true)
		if err != nil {
			utils.LogError(err.Error())
		}

		unusedUmwl()
	},
}
