package labelgroupimport

import (
	"fmt"
	"os"
	"strings"

	"github.com/brian1917/workloader/cmd/labelgroupexport"

	"github.com/brian1917/illumioapi"
	"github.com/brian1917/workloader/utils"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Global variables
var csvFile string
var provision, updatePCE, noPrompt, ignoreHref bool
var pce illumioapi.PCE
var err error

// Struct for entries
type entry struct {
	csvLine    int
	labelGroup illumioapi.LabelGroup
	// pendingSubGroups holds the lookup keys (lowercase key + name) of member label groups
	// that do not exist in the PCE yet and will be created earlier in the same run.
	pendingSubGroups []string
}

// lgID returns the lookup key used for label groups (lowercase key + name)
func lgID(key, name string) string {
	return strings.ToLower(key) + name
}

// droppedEntry is an entry that cannot be processed because of its pending subgroups
type droppedEntry struct {
	entry  entry
	reason string
}

// orderLabelGroups sorts new label groups so each is created after any new label groups it uses
// as members. New label groups in a circular reference, or that depend on a label group that
// will not be created, are dropped. Updates that depend on a dropped label group are also dropped.
func orderLabelGroups(newLGs, updatedLGs []entry) (orderedNew, keptUpdates []entry, dropped []droppedEntry) {
	byID := make(map[string]int)
	for i, e := range newLGs {
		if _, exists := byID[lgID(e.labelGroup.Key, e.labelGroup.Name)]; !exists {
			byID[lgID(e.labelGroup.Key, e.labelGroup.Name)] = i
		}
	}

	const (
		unvisited = iota
		visiting
		done
	)
	state := make([]int, len(newLGs))
	ok := make([]bool, len(newLGs))

	var resolve func(i int) bool
	resolve = func(i int) bool {
		switch state[i] {
		case done:
			return ok[i]
		case visiting:
			return false
		}
		state[i] = visiting
		ok[i] = true
		reason := ""
		for _, dep := range newLGs[i].pendingSubGroups {
			depIndex, exists := byID[dep]
			if !exists {
				ok[i] = false
				reason = fmt.Sprintf("member label group %s will not be created", dep)
				break
			}
			if state[depIndex] == visiting {
				ok[i] = false
				reason = fmt.Sprintf("circular member label group reference involving %s", newLGs[depIndex].labelGroup.Name)
				break
			}
			if !resolve(depIndex) {
				ok[i] = false
				reason = fmt.Sprintf("member label group %s will not be created", newLGs[depIndex].labelGroup.Name)
				break
			}
		}
		state[i] = done
		if ok[i] {
			orderedNew = append(orderedNew, newLGs[i])
		} else {
			dropped = append(dropped, droppedEntry{entry: newLGs[i], reason: reason})
		}
		return ok[i]
	}

	for i := range newLGs {
		resolve(i)
	}

UpdateEntries:
	for _, e := range updatedLGs {
		for _, dep := range e.pendingSubGroups {
			if depIndex, exists := byID[dep]; !exists || !ok[depIndex] {
				dropped = append(dropped, droppedEntry{entry: e, reason: fmt.Sprintf("member label group %s will not be created", dep)})
				continue UpdateEntries
			}
		}
		keptUpdates = append(keptUpdates, e)
	}

	return orderedNew, keptUpdates, dropped
}

// resolvePending adds the hrefs of label groups created in this run to the entry's subgroups.
// It returns the id of the first pending subgroup that was not created.
func resolvePending(e *entry, createdHrefs map[string]string) (missing string) {
	for _, dep := range e.pendingSubGroups {
		href, ok := createdHrefs[dep]
		if !ok {
			return dep
		}
		e.labelGroup.SubGroups = append(e.labelGroup.SubGroups, &illumioapi.SubGroups{Href: href})
	}
	return ""
}

func init() {
	LabelGroupImportCmd.Flags().BoolVarP(&provision, "provision", "p", false, "Provision changes.")
	LabelGroupImportCmd.Flags().BoolVar(&ignoreHref, "ignore-href", false, "ignore the href column in the CSV. useful when importing a CSV exported from a different PCE.")
	LabelGroupImportCmd.Flags().SortFlags = false
}

// LabelGroupImportCmd runs the upload command
var LabelGroupImportCmd = &cobra.Command{
	Use:   "labelgroup-import [csv file to import]",
	Short: "Create and modify label groups from a CSV file.",
	Long: `
Create and modify label groups from a CSV file.

The input file requires headers and matches fields to header values. The orer of the headers are not required. The following headers can be used:
` + "\r\n- " + labelgroupexport.HeaderName + "\r\n" +
		"- " + labelgroupexport.HeaderHref + "\r\n" +
		"- " + labelgroupexport.HeaderDescription + "\r\n" +
		"- " + labelgroupexport.HeaderKey + "\r\n" +
		"- " + labelgroupexport.HeaderMemberLabels + "\r\n" +
		"- " + labelgroupexport.HeaderMemberLabelGroups + "\r\n" + `

It's recommended to start with the output from labelgroup-export command and use that as provided input.

If an href is provided, the label group will be modified. If no href is provided, the label group will be created.

Other columns are alloewd but will be ignored.

Member label values and member label groups should be separated by a semi-colon.

Member label groups can reference label groups created in the same CSV. workloader creates them in dependency order regardless of row order.

Recommended to run without --update-pce first to log of what will change. If --update-pce is used, import will create labels without prompt, but it will not create/update workloads without user confirmation, unless --no-prompt is used.`,

	Run: func(cmd *cobra.Command, args []string) {

		pce, err = utils.GetTargetPCE(true)
		if err != nil {
			utils.Logger.Fatalf("error getting PCE for csv command - %s", err)
		}

		// Set the CSV file
		if len(args) != 1 {
			fmt.Println("command requires 1 argument for the csv file. see usage help.")
			os.Exit(0)
		}
		csvFile = args[0]

		// Get the debug value from viper
		updatePCE = viper.GetBool("update_pce")
		noPrompt = viper.GetBool("no_prompt")

		labelGroupImport()
	},
}

func labelGroupImport() {

	// Parse the CSV
	csvData, err := utils.ParseCSV(csvFile)
	if err != nil {
		utils.LogError(err.Error())
	}

	// Load the PCE
	apiResps, err := pce.Load(illumioapi.LoadInput{LabelGroups: true})
	utils.LogMultiAPIResp(apiResps)
	if err != nil {
		utils.LogError(err.Error())
	}

	// Start slices to hold the results
	newLabelGroups := []entry{}
	updatedLabelGroups := []entry{}

	if len(csvData) == 0 {
		utils.LogError("csv file is empty")
	}

	// Headers
	headers := make(map[string]*int)
	for i, l := range csvData[0] {
		x := i
		headers[l] = &x
	}

	// isCreate returns true if the line will create a new label group
	isCreate := func(line []string) bool {
		return ignoreHref || headers[labelgroupexport.HeaderHref] == nil || line[*headers[labelgroupexport.HeaderHref]] == ""
	}

	// Collect the label groups that will be created in this run so they can be used as member label groups
	newLGIDs := make(map[string]bool)
	nameCol, keyCol := headers[labelgroupexport.HeaderName], headers[labelgroupexport.HeaderKey]
	if nameCol != nil && keyCol != nil {
		for _, line := range csvData[1:] {
			if isCreate(line) && line[*nameCol] != "" && line[*keyCol] != "" {
				newLGIDs[lgID(line[*keyCol], line[*nameCol])] = true
			}
		}
	}

	// Process each row of the CSV
CSVEntries:
	for i, line := range csvData {
		var key string

		// Skip the header row
		if i == 0 {
			continue
		}

		// If the href header is not present, blank, or ignored, it's created
		if isCreate(line) {
			pending := []string{}
			newLG := illumioapi.LabelGroup{}

			// Name
			if val, ok := headers[labelgroupexport.HeaderName]; !ok || line[*val] == "" {
				utils.LogWarning(fmt.Sprintf("csv line %d - name field cannot be blank for new label group. skipping entry", i+1), true)
				continue CSVEntries
			} else {
				newLG.Name = line[*val]
			}

			// Description
			if val, ok := headers[labelgroupexport.HeaderDescription]; ok {
				newLG.Description = line[*val]
			}

			// Key
			if val, ok := headers[labelgroupexport.HeaderKey]; !ok || line[*val] == "" {
				utils.LogWarning(fmt.Sprintf("csv line %d - key field cannot be blank for new label group. skipping entry", i+1), true)
				continue CSVEntries
			} else {
				key = strings.ToLower(line[*val])
				if key != "role" && key != "app" && key != "loc" && key != "env" {
					utils.LogWarning(fmt.Sprintf("csv line %d - key field must be either role, app, env, or loc", i+1), true)
				}
				newLG.Key = line[*val]
			}

			// Member Labels
			if val, ok := headers[labelgroupexport.HeaderMemberLabels]; ok && line[*val] != "" {
				labels := strings.Split(strings.Replace(line[*val], "; ", ";", -1), ";")
				for _, l := range labels {
					if pceLabel, check := pce.Labels[key+l]; !check {
						utils.LogWarning(fmt.Sprintf("csv line %d - the label %s (%s) does not exist. skipping entry.", i+1, l, key), true)
						continue CSVEntries
					} else {
						newLG.Labels = append(newLG.Labels, &illumioapi.Label{Href: pceLabel.Href})
					}
				}
			}

			// Member Label Groups
			if val, ok := headers[labelgroupexport.HeaderMemberLabelGroups]; ok && line[*val] != "" {
				labelGroups := strings.Split(strings.Replace(line[*val], "; ", ";", -1), ";")
				for _, lg := range labelGroups {
					if pceLabelGroup, check := pce.LabelGroups[key+lg]; check {
						newLG.SubGroups = append(newLG.SubGroups, &illumioapi.SubGroups{Href: pceLabelGroup.Href})
					} else if newLGIDs[key+lg] {
						pending = append(pending, key+lg)
						utils.LogInfo(fmt.Sprintf("csv line %d - member label group %s (%s) will be created in this run.", i+1, lg, key), false)
					} else {
						utils.LogWarning(fmt.Sprintf("csv line %d - the label group %s (%s) does not exist. skipping entry.", i+1, lg, key), true)
						continue CSVEntries
					}
				}
			}

			// Add to the new labelgroup slice
			newLabelGroups = append(newLabelGroups, entry{csvLine: i + 1, labelGroup: newLG, pendingSubGroups: pending})
			utils.LogInfo(fmt.Sprintf("csv line %d - %s - will be created.", i+1, line[*headers[labelgroupexport.HeaderName]]), false)

		} else {
			// The label group HREF field is present and the value is provided,
			var pceLabelGroup illumioapi.LabelGroup
			var check bool
			if pceLabelGroup, check = pce.LabelGroups[line[*headers[labelgroupexport.HeaderHref]]]; !check {
				utils.LogWarning(fmt.Sprintf("csv line %d - href is provided but it does not exist in the PCE. skipping entry.", i+1), true)
				continue CSVEntries
			}

			// Set update to false
			update := false

			// Name
			if val, ok := headers[labelgroupexport.HeaderName]; ok {
				if line[*val] != pceLabelGroup.Name {
					update = true
					utils.LogInfo(fmt.Sprintf("csv line %d - the name will change from %s to %s.", i+1, pceLabelGroup.Name, line[*val]), false)
					pceLabelGroup.Name = line[*val]
				}
			}

			// Description
			if val, ok := headers[labelgroupexport.HeaderDescription]; ok {
				if line[*val] != pceLabelGroup.Description {
					update = true
					utils.LogInfo(fmt.Sprintf("csv line %d - the description will change from %s to %s.", i+1, pceLabelGroup.Description, line[*val]), false)
					pceLabelGroup.Description = line[*val]
				}
			}

			// Key
			if val, ok := headers[labelgroupexport.HeaderKey]; ok {
				key = strings.ToLower(line[*val])
				if line[*val] != pceLabelGroup.Key {
					utils.LogWarning(fmt.Sprintf("csv line %d - the key cannot be changed for an existing label group. skipping entry.", i+1), true)
					continue CSVEntries
				}
			}
			if key == "" {
				key = strings.ToLower(pceLabelGroup.Key)
			}

			// Member labels
			// Create maps for the labels in the PCE and the labels in the CSV entry
			// Set the label update to false
			labelUpdate := false
			pceLabels := make(map[string]bool)
			csvLabels := make(map[string]bool)

			if val, ok := headers[labelgroupexport.HeaderMemberLabels]; ok && line[*val] != "" {

				// Populate PCE labels
				for _, l := range pceLabelGroup.Labels {
					pceLabels[pce.Labels[l.Href].Value] = true
				}
				// Populate CSV labels
				for _, l := range strings.Split(strings.Replace(line[*val], "; ", ";", -1), ";") {
					csvLabels[l] = true
				}

				// Check if CSV labels are in the PCE
				for l := range csvLabels {
					if !pceLabels[l] {
						// Check if the label exists
						if _, check := pce.Labels[key+l]; !check {
							utils.LogWarning(fmt.Sprintf("csv line %d - %s(%s) does not exist in the PCE as a label. skipping entry.", i+1, l, key), true)
							continue CSVEntries
						}
						labelUpdate = true
						utils.LogInfo(fmt.Sprintf("csv line %d - %s label is in the CSV but not in the PCE. It will be added.", i+1, l), false)
					}
				}
				// Check if PCE labels are in the CSV
				for l := range pceLabels {
					if !csvLabels[l] {
						labelUpdate = true
						utils.LogInfo(fmt.Sprintf("csv line %d - %s label is not in the CSV but is in the PCE. It will be removed.", i+1, l), false)
					}
				}

				// If we are updating the labels, replace with newLabels
				var newLabels []*illumioapi.Label
				if labelUpdate {
					update = true
					for l := range csvLabels {
						newLabels = append(newLabels, &illumioapi.Label{Href: pce.Labels[key+l].Href})
					}
					pceLabelGroup.Labels = newLabels
				} else {
					for _, l := range pceLabelGroup.Labels {
						newLabels = append(newLabels, &illumioapi.Label{Href: l.Href})
					}
					pceLabelGroup.Labels = newLabels
				}
			}

			// Member Sub Groups
			// Create maps for the subgroups in the PCE and the labels in the CSV entry and set sgUpdate to false
			sgUpdate := false
			pceSGs := make(map[string]bool)
			csvSGs := make(map[string]bool)

			if val, ok := headers[labelgroupexport.HeaderMemberLabelGroups]; ok && line[*val] != "" {
				for _, sg := range pceLabelGroup.SubGroups {
					pceSGs[pce.LabelGroups[sg.Href].Name] = true
				}
				for _, sg := range strings.Split(strings.Replace(line[*val], "; ", ";", -1), ";") {
					csvSGs[sg] = true
				}

				// Check if CSV groups are in the PCE
				for sg := range csvSGs {
					if !pceSGs[sg] {
						// Check if the group exists or will be created in this run
						if _, check := pce.LabelGroups[key+sg]; !check && !newLGIDs[key+sg] {
							utils.LogWarning(fmt.Sprintf("csv line %d - %s(%s) does not exist in the PCE as a label group. skipping entry.", i+1, sg, key), true)
							continue CSVEntries
						}
						sgUpdate = true
						utils.LogInfo(fmt.Sprintf("csv line %d - %s subgroup is in the CSV but not in the PCE. It will be added.", i+1, sg), false)
						if _, check := pce.LabelGroups[key+sg]; !check {
							utils.LogInfo(fmt.Sprintf("csv line %d - member label group %s (%s) will be created in this run.", i+1, sg, key), false)
						}
					}
				}
				// Check if PCE labels are in the CSV
				for sg := range pceSGs {
					if !csvSGs[sg] {
						sgUpdate = true
						utils.LogInfo(fmt.Sprintf("csv line %d - %s subgroup is not in the CSV but is in the PCE. It will be removed.", i+1, sg), false)
					}
				}
			}
			// If we are updating the sub groups, replace with newSubGroups
			var newSubGroups []*illumioapi.SubGroups
			pending := []string{}
			if sgUpdate {
				update = true
				for sg := range csvSGs {
					if pceSG, check := pce.LabelGroups[key+sg]; check {
						newSubGroups = append(newSubGroups, &illumioapi.SubGroups{Href: pceSG.Href})
					} else {
						pending = append(pending, key+sg)
					}
				}
				pceLabelGroup.SubGroups = newSubGroups
			} else {
				for _, sg := range pceLabelGroup.SubGroups {
					newSubGroups = append(newSubGroups, &illumioapi.SubGroups{Href: sg.Href})
				}
				pceLabelGroup.SubGroups = newSubGroups
			}

			// If update is set to true, add it to the slice
			if update {
				updatedLabelGroups = append(updatedLabelGroups, entry{csvLine: i + 1, labelGroup: pceLabelGroup, pendingSubGroups: pending})
			}
		}
	}

	// Order new label groups so member label groups are created first and drop entries that cannot be resolved
	var dropped []droppedEntry
	newLabelGroups, updatedLabelGroups, dropped = orderLabelGroups(newLabelGroups, updatedLabelGroups)
	for _, d := range dropped {
		utils.LogWarning(fmt.Sprintf("csv line %d - %s - %s. skipping entry.", d.entry.csvLine, d.entry.labelGroup.Name, d.reason), true)
	}

	// End run if we have nothing to do
	if len(newLabelGroups) == 0 && len(updatedLabelGroups) == 0 {
		utils.LogInfo("nothing to be done.", true)

		return
	}

	// If updatePCE is disabled, we are just going to alert the user what will happen and log
	if !updatePCE {
		utils.LogInfo(fmt.Sprintf("workloader identified %d label groups to create and %d label groups to update. See workloader.log for all identified changes. To do the import, run again using --update-pce flag", len(newLabelGroups), len(updatedLabelGroups)), true)

		return
	}

	// If updatePCE is set, but not noPrompt, we will prompt the user.
	if updatePCE && !noPrompt {
		var prompt string
		fmt.Printf("[PROMPT] - workloader will create %d label groups and update %d label groups in %s (%s). Do you want to run the import (yes/no)? ", len(newLabelGroups), len(updatedLabelGroups), pce.FriendlyName, viper.GetString(pce.FriendlyName+".fqdn"))

		fmt.Scanln(&prompt)
		if strings.ToLower(prompt) != "yes" {
			utils.LogInfo(fmt.Sprintf("prompt denied for creating %d label groups and updating %d label groups.", len(newLabelGroups), len(updatedLabelGroups)), true)

			return
		}
	}

	skipped := 0
	createdLGs := 0
	updatedLGs := 0
	provisionableLGs := []string{}
	// Hrefs of label groups created in this run (keyed by lowercase key + name)
	createdHrefs := make(map[string]string)

	// Create Label Groups
	for _, newLG := range newLabelGroups {
		if missing := resolvePending(&newLG, createdHrefs); missing != "" {
			utils.LogWarning(fmt.Sprintf("csv line %d - %s - member label group %s was not created. skipping entry.", newLG.csvLine, newLG.labelGroup.Name, missing), true)
			skipped++
			continue
		}
		lg, a, err := pce.CreateLabelGroup(newLG.labelGroup)
		utils.LogAPIResp("CreateLabelGroup", a)
		if err != nil && a.StatusCode != 406 {
			utils.LogError(fmt.Sprintf("ending run - %d label groups created - %d label groups updated.", createdLGs, updatedLGs))
		}
		if a.StatusCode == 406 {
			utils.LogWarning(fmt.Sprintf("csv line %d - %s - 406 Not Acceptable - See workloader.log for more details", newLG.csvLine, newLG.labelGroup.Name), true)
			utils.LogWarning(a.RespBody, false)
			skipped++
		}
		if err == nil {
			utils.LogInfo(fmt.Sprintf("csv line %d - %s created - status code %d", newLG.csvLine, lg.Name, a.StatusCode), true)
			createdLGs++
			provisionableLGs = append(provisionableLGs, lg.Href)
			createdHrefs[lgID(newLG.labelGroup.Key, newLG.labelGroup.Name)] = lg.Href
		}
	}

	// Update Label Groups
	for _, updateLG := range updatedLabelGroups {
		if missing := resolvePending(&updateLG, createdHrefs); missing != "" {
			utils.LogWarning(fmt.Sprintf("csv line %d - %s - member label group %s was not created. skipping entry.", updateLG.csvLine, updateLG.labelGroup.Name, missing), true)
			skipped++
			continue
		}
		a, err := pce.UpdateLabelGroup(updateLG.labelGroup)
		utils.LogAPIResp("UpdateLabelGroup", a)
		if err != nil && a.StatusCode != 406 {
			utils.LogError(fmt.Sprintf("ending run - %d label groups created - %d label groups updated.", createdLGs, updatedLGs))
			utils.LogError(err.Error())
		}
		if a.StatusCode == 406 {
			utils.LogWarning(fmt.Sprintf("csv line %d - %s - 406 Not Acceptable - See workloader.log for more details", updateLG.csvLine, updateLG.labelGroup.Name), true)
			utils.LogWarning(a.RespBody, false)
			skipped++
		}
		if err == nil {
			utils.LogInfo(fmt.Sprintf("csv line %d - %s updated - status code %d", updateLG.csvLine, updateLG.labelGroup.Name, a.StatusCode), true)
			updatedLGs++
			provisionableLGs = append(provisionableLGs, updateLG.labelGroup.Href)
		}
	}

	// Provision
	if provision {
		a, err := pce.ProvisionHref(provisionableLGs, "workloader labelgroup-import")
		utils.LogAPIResp("ProvisionHrefs", a)
		if err != nil {
			utils.LogError(err.Error())
		}
		utils.LogInfo(fmt.Sprintf("provisioning successful - status code %d", a.StatusCode), true)
	}

}
