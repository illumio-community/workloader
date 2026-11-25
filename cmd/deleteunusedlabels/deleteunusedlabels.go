package deleteunusedlabels

import (
	"fmt"

	"github.com/brian1917/illumioapi/v2"
	"github.com/brian1917/workloader/cmd/deletehrefs"
	"github.com/brian1917/workloader/utils"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// LabelsDeleteUnusedCmd deletes labels that are not in use
var LabelsDeleteUnusedCmd = &cobra.Command{
	Use:   "labels-delete-unused",
	Short: "Delete labels that are not used.",
	Long: `
Delete labels that are not used.

Label usage is determined by the PCE's label usage API. Any label not used by a workload, container workload, ruleset, label group, pairing profile, permission, virtual service, virtual server, enforcement boundary, or other policy object is identified for deletion.

Without --update-pce, the command reports how many labels would be deleted. With --update-pce, the user is prompted before deleting unless --no-prompt is also set.`,
	Run: func(cmd *cobra.Command, args []string) {
		pce, err := utils.GetTargetPCEV2(true)
		if err != nil {
			utils.LogError(err.Error())
		}

		labelsDeleteUnused(pce, viper.GetBool("update_pce"), viper.GetBool("no_prompt"))
	},
}

func labelsDeleteUnused(pce illumioapi.PCE, updatePCE, noPrompt bool) {

	// Get all labels with usage
	a, err := pce.GetLabels(map[string]string{"usage": "true"})
	utils.LogAPIRespV2("GetLabels", a)
	if err != nil {
		utils.LogError(err.Error())
	}

	// Build the list of unused label hrefs
	hrefs := []string{}
	for _, l := range pce.LabelsSlice {
		if l.LabelUsage == nil || *l.LabelUsage != (illumioapi.LabelUsage{}) {
			continue
		}
		utils.LogInfo(fmt.Sprintf("%s (%s) - %s - not in use", l.Value, l.Key, l.Href), false)
		hrefs = append(hrefs, l.Href)
	}

	if len(hrefs) == 0 {
		utils.LogInfo("no unused labels found.", true)
		return
	}

	// Pass into the delete hrefs command
	deletehrefs.DeleteHrefs(deletehrefs.Input{
		PCE:       pce,
		Hrefs:     hrefs,
		UpdatePCE: updatePCE,
		NoPrompt:  noPrompt,
	})
}
